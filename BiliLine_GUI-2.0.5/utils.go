package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"time"

	"golang.org/x/exp/slog"

	"github.com/vtb-link/bianka/live"

	"github.com/vtb-link/bianka/proto"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

//go:embed Resource/Wx.jpg
var WxJpg []byte

//go:embed Resource/Alipay.jpg
var AliPayJpg []byte

//go:embed Resource/AlipayRedPack.jpg
var AliPayRedPack []byte

func CalculateTimeDifference(timeString string) time.Duration {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return 0
	}
	layout := "2006-01-02 15:04:05"
	t, err := time.ParseInLocation(layout, timeString, location)
	if err != nil {
		slog.Error("时间解析失败", err)
		return 0
	}
	// 计算当前时间与给定时间之间的差异
	diff := time.Since(t)
	return diff
}

func RemoveTags(str string) string {
	// 创建正则表达式匹配模式
	re := regexp.MustCompile(`<.*?>`)
	// 使用空字符串替换匹配到的部分
	result := re.ReplaceAllString(str, "")
	return result
}

// 重构SendLineToWs函数消除重复代码
func SendLineToWs(NormalLine Line, Gift GiftLine, LineType int) {
	fmt.Printf("SendLineToWs 开始")
	var send WsPack
	var hasContent bool

	switch {
	case len(NormalLine.OpenID) > 0:
		send = WsPack{
			OpMessage: OpAdd,
			LineType:  LineType,
			Line:      NormalLine,
		}
		hasContent = true
	case len(Gift.OpenID) > 0:
		send = WsPack{
			OpMessage: OpAdd,
			LineType:  LineType,
			GiftLine:  Gift,
		}
		hasContent = true
	default:
		slog.Debug("发送空数据包", slog.Any("NormalLine", NormalLine), slog.Any("Gift", Gift))
		return
	}

	if hasContent {
		SendWsJson, err := json.Marshal(send)
		if err != nil {
			slog.Error("WebSocket数据封禁失败", err, slog.Any("send", send))
			return
		}
		QueueChatChan <- SendWsJson
	}
	fmt.Printf("SendLineToWs 结束")
}

func SendDmToWs(Dm *proto.CmdDanmuData) {
	SendDmWsJson, err := json.Marshal(Dm)
	if err != nil {
		return
	}
	DmChatChan <- SendDmWsJson
}

func SendMusicServer(Path, Keyword string) {
	for i := 0; i < 3; i++ {
		get, err := http.Get("http://127.0.0.1:99/" + Path + "?keyword=" + Keyword)
		if err != nil {
			return
		}
		if get.StatusCode == 200 {
			break
		}
	}
}

func SendDelToWs(LineType, index int, OpenId string) {
	Send := WsPack{
		OpMessage: OpDelete,
		Index:     index,
		LineType:  LineType,
		Line: Line{
			OpenID: OpenId,
		},
	}
	SendWsJson, err := json.Marshal(Send)
	if err != nil {
		return
	}
	QueueChatChan <- SendWsJson
}

func SendWhereToWs(OpenId string) {
	Send := WsPack{
		OpMessage: OpWhere,
	}
	SendWsJson, err := json.Marshal(Send)
	if err != nil {
		return
	}
	QueueChatChan <- SendWsJson
}

// 新增函数：发送状态更新到WebSocket
func sendStatusUpdate(openID string, isOnline bool) {
	Send := WsPack{
		OpMessage: OpUpdateState, // 状态更新操作码
		Line: Line{
			OpenID:   openID,
			IsOnline: isOnline,
		},
	}
	SendWsJson, err := json.Marshal(Send)
	if err != nil {
		return
	}
	QueueChatChan <- SendWsJson
}

func UpdateUserStatus(OpenId string, isOnlie bool) error {
	if OpenId == "" {
		return fmt.Errorf("empty OpenID provided")
	}

	var lineType int
	var index int
	var exists bool

	// 检查是否存在于礼物队列
	if index, exists = line.GiftIndex[OpenId]; exists && index > 0 && index <= len(line.GiftLine) {
		line.GiftLine[index-1].IsOnline = isOnlie
		lineType = GiftLineType
	} else if index, exists = line.CommonIndex[OpenId]; exists && index > 0 && index <= len(line.CommonLine) {
		line.CommonLine[index-1].IsOnline = isOnlie
		lineType = CommonLineType
	} else {
		return fmt.Errorf("未找到用户: %s", OpenId)
	}

	// 保存数据并发送状态更新
	line.UpdateIndex(lineType)        // 更新索引
	SetLine(line)                     // 持久化存储
	sendStatusUpdate(OpenId, isOnlie) // WebSocket通知
	return nil
}

func DeleteLine(OpenId string) error {
	if OpenId == "" {
		return fmt.Errorf("empty OpenID provided")
	}

	var err error
	var idx int
	var ok bool
	var lineType int

	if idx, ok = line.GiftIndex[OpenId]; ok && idx > 0 && idx <= len(line.GiftLine) {
		line.GiftLine = append(line.GiftLine[:idx-1], line.GiftLine[idx:]...)
		delete(line.GiftIndex, OpenId)
		lineType = GiftLineType
		err = nil
	} else if idx, ok = line.CommonIndex[OpenId]; ok && idx > 0 && idx <= len(line.CommonLine) {
		line.CommonLine = append(line.CommonLine[:idx-1], line.CommonLine[idx:]...)
		delete(line.CommonIndex, OpenId)
		lineType = CommonLineType
		err = nil
	} else {
		return fmt.Errorf("user not found or invalid index for OpenID: %s", OpenId)
	}

	// 使用指针接收者更新索引
	line.UpdateIndex(lineType)
	SetLine(line) // 保存完整line状态到文件
	SendDelToWs(lineType, idx-1, OpenId)
	return err
}

func DeleteFirst() error {
	if len(line.GiftLine) > 0 {
		return DeleteLine(line.GiftLine[0].OpenID)
	}
	if len(line.CommonLine) > 0 {
		return DeleteLine(line.CommonLine[0].OpenID)
	}
	return errors.New("no users to delete")
}

func assistUI() *fyne.Container {
	Wx := canvas.NewImageFromReader(bytes.NewReader(WxJpg), "Wx.jpg")
	Wx.FillMode = canvas.ImageFillOriginal
	AliPay := canvas.NewImageFromReader(bytes.NewReader(AliPayJpg), "Alipay.jpg")
	AliPay.FillMode = canvas.ImageFillOriginal
	AliPayRed := canvas.NewImageFromReader(bytes.NewReader(AliPayRedPack), "AliPayRedPack.jpg")
	AliPayRed.FillMode = canvas.ImageFillOriginal
	Cont := container.NewHBox(Wx, AliPay, AliPayRed)
	return Cont
}

// func DisplaySpecialUserListUI() *fyne.Container {
// 	SpecialUserBoxItem := make(map[string]*fyne.Container)

// 	Cont := container.NewVBox()
// 	for k, v := range SpecialUserList {
// 		var timeCanvas = canvas.NewText(time.Unix(v.EndTime, 0).Format("2006-01-02 15:04:05"), color.White)
// 		SpecialUserBoxItem[k] = container.NewHBox(
// 			canvas.NewText(v.UserName, color.White),
// 			timeCanvas,
// 			widget.NewButton("删除", func() {
// 				delete(SpecialUserList, k)
// 				globalConfiguration.SpecialUserList = SpecialUserList
// 				SetConfig(globalConfiguration)
// 				Cont.Remove(SpecialUserBoxItem[k])
// 			}),
// 			widget.NewButton("修改截止时间", func() {
// 				var selectedYear, selectedMonth, selectedDay string
// 				dialog.ShowCustomConfirm("选择截止日期", "确定", "取消", NewDatePicker(&selectedYear, &selectedMonth, &selectedDay), func(b bool) {
// 					timestamp, err := ConvertToTimestamp(selectedYear, selectedMonth, selectedDay)
// 					if err != nil {
// 						dialog.ShowError(errors.New("时间选择错误"), CtrlWindows)
// 					}
// 					SpecialUserList[k] = SpecialUserStruct{
// 						EndTime:  timestamp,
// 						UserName: v.UserName,
// 					}
// 					globalConfiguration.SpecialUserList = SpecialUserList
// 					SetConfig(globalConfiguration)
// 					timeCanvas.Text = time.Unix(timestamp, 0).Format("2006-01-02 15:04:05")
// 					Cont.Refresh()
// 				}, SpecialUserSetWindows)
// 			}),
// 		)
// 		Cont.Add(SpecialUserBoxItem[k])
// 	}
// 	return Cont
// }

func randomInt(min, max int) int {
	rand.New(rand.NewSource(time.Now().UnixNano()))
	return rand.Intn(max-min+1) + min
}

func CleanOldVersion() {
	_, err := os.Stat("./Version " + NowVersion)
	if err != nil {
		_ = os.Remove("./line.json")
		_ = os.Remove("./lineConfig.json")

		_, _ = os.Create("./Version " + NowVersion)
		return
	}
}

func AgreeOpenUrl(url string) error {
	var (
		cmd  string
		args []string
	)

	switch runtime.GOOS {
	case "windows":
		cmd, args = "cmd", []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	case "Agree":
		cmd = "Agree"
		os.Exit(0)
	default:
		// "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

func Restart() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("无法获取可执行文件路径:", err)
		return
	}
	// 启动新进程来替换当前进程
	cmd := exec.Command(exePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		slog.Error("进程启动失败", err)
		return
	}
	// 新增退出旧进程逻辑
	os.Exit(0)
}

func NewHeartbeat(client *live.Client, GameId string, CloseChan chan bool) {
	tk := time.NewTicker(time.Second * 20)
	go func() {
	loop:
		for {
			select {
			case <-tk.C:
				if err := client.AppHeartbeat(GameId); err != nil {
					slog.Error("Heartbeat fail", err)
				} else {
					slog.Info("Heartbeat Success", GameId)
				}
			case <-CloseChan:

				break loop
			}
		}
		tk.Stop()
	}()
}

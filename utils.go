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

// 新增函数：发送状态更新到WebSocket
func sendStatusUpdate(openID string, isOnline bool) {
	updateMsg := map[string]interface{}{
		"OpMessage": 3,        // 状态更新操作码
		"OpenID":    openID,   // 用户唯一标识
		"is_online": isOnline, // 新的在线状态
	}
	SendWsJson, err := json.Marshal(updateMsg)
	if err != nil {
		slog.Error("序列化状态更新消息失败", err)
		return
	}
	QueueChatChan <- SendWsJson
}

// 修改DeleteLine函数，增强错误检查和日志记录
func DeleteLine(OpenId string) error {
	// 添加防御性检查
	if OpenId == "" {
		slog.Error("尝试删除空OpenID")
		return fmt.Errorf("empty OpenID provided")
	}

	if idx, ok := line.GuardIndex[OpenId]; ok {
		if idx > 0 && idx <= len(line.GuardLine) {
			line.GuardLine = append(line.GuardLine[:idx-1], line.GuardLine[idx:]...)
			SendDelToWs(GuardLineType, idx-1, OpenId)
			delete(line.GuardIndex, OpenId)
			line.UpdateIndex(GuardLineType)
			SetLine(line)
			return nil
		}
		slog.Error("GuardLine索引越界", slog.Int("index", idx), slog.String("OpenID", OpenId))
		return fmt.Errorf("guard index out of bounds for OpenID: %s", OpenId)
	}

	if idx, ok := line.GiftIndex[OpenId]; ok {
		if idx > 0 && idx <= len(line.GiftLine) {
			line.GiftLine = append(line.GiftLine[:idx-1], line.GiftLine[idx:]...)
			SendDelToWs(GiftLineType, idx-1, OpenId)
			delete(line.GiftIndex, OpenId)
			line.UpdateIndex(GiftLineType)
			SetLine(line)
			return nil
		}
		slog.Error("GiftLine索引越界", slog.Int("index", idx), slog.String("OpenID", OpenId))
		return fmt.Errorf("gift index out of bounds for OpenID: %s", OpenId)
	}

	if idx, ok := line.CommonIndex[OpenId]; ok {
		if idx > 0 && idx <= len(line.CommonLine) {
			line.CommonLine = append(line.CommonLine[:idx-1], line.CommonLine[idx:]...)
			SendDelToWs(CommonLineType, idx-1, OpenId)
			delete(line.CommonIndex, OpenId)
			line.UpdateIndex(CommonLineType)
			SetLine(line)
			return nil
		}
		slog.Error("CommonLine索引越界", slog.Int("index", idx), slog.String("OpenID", OpenId))
		return fmt.Errorf("common index out of bounds for OpenID: %s", OpenId)
	}

	slog.Warn("未找到用户或无效索引", slog.String("OpenID", OpenId))
	return fmt.Errorf("user not found or invalid index for OpenID: %s", OpenId)
}

func DeleteFirst() error {
	if len(line.GuardLine) > 0 {
		return DeleteLine(line.GuardLine[0].OpenID)
	}
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
	tk := time.NewTicker(time.Second * 10)
	go func() {
		for {
			select {
			case <-tk.C:
				if err := client.AppHeartbeat(GameId); err != nil {
					slog.Error("Heartbeat fail", err)
				} else {
					slog.Info("Heartbeat Success", GameId)
				}
			case <-CloseChan:
				tk.Stop()
				break
			}
		}
	}()
}

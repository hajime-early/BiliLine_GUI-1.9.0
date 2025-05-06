package main

import (
	"fmt"
	"reflect"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"sync"
	"time"

	"golang.org/x/exp/slog"

	"github.com/vtb-link/bianka/basic"
	"github.com/vtb-link/bianka/live"
	"github.com/vtb-link/bianka/proto"
)

var (
	lineMu sync.RWMutex // 添加互斥锁保护共享数据
)

func messageHandle(ws *basic.WsClient, msg *proto.Message) error {
	cmd, data, err := proto.AutomaticParsingMessageCommand(msg.Payload())
	if err != nil {
		return err
	}

	switch cmd {
	case proto.CmdLiveOpenPlatformDanmu:
		DanmuData := data.(*proto.CmdDanmuData)
		slog.Info(DanmuData.Uname, DanmuData.Msg)
		ResponseQueCtrl(DanmuData)

	case proto.CmdLiveOpenPlatformSendGift:
		GiftData := data.(*proto.CmdSendGiftData)
		fmt.Printf("检测到礼物：%v  礼物价值(电池)：%v 礼物数量：%v 是否为付费：%v \n",
			GiftData.GiftName, GiftData.Price, GiftData.GiftNum, GiftData.Paid)

		if !globalConfiguration.AutoJoinGiftLine {
			break
		}

		//如果不是付费礼物则不执行以下代码。
		if !GiftData.Paid {
			break
		}

		lineMu.Lock() // 加锁保护共享数据
		defer lineMu.Unlock()

		//检测送礼用户在不在排队列表，在的话就删除排队列表的数据添加到礼物列表
		if idx, exists := line.CommonIndex[GiftData.OpenID]; exists && idx > 0 {
			// 确保索引有效
			if idx <= len(line.CommonLine) {
				// 从CommonLine删除
				line.CommonLine = append(line.CommonLine[:idx-1], line.CommonLine[idx:]...)

				// 重建索引
				delete(line.CommonIndex, GiftData.OpenID)
				for i, user := range line.CommonLine {
					line.CommonIndex[user.OpenID] = i + 1
				}
			} else {
				// 索引无效时清除错误索引
				delete(line.CommonIndex, GiftData.OpenID)
			}
		}

		giftValue := float64(GiftData.Price*GiftData.GiftNum) / 100.0 // 修改处：除以100

		if idx, exists := line.GiftIndex[GiftData.OpenID]; exists {
			line.GiftLine[idx-1].GiftPrice += giftValue
			fmt.Printf("目前用户：%v 累计礼物价值为：%v \n", GiftData.Uname, line.GiftLine[idx-1].GiftPrice)
		} else {
			lineTemp := GiftLine{
				OpenID:     GiftData.OpenID,
				UserName:   GiftData.Uname,
				Avatar:     GiftData.Uface,
				PrintColor: globalConfiguration.GiftPrintColor,
				GiftPrice:  giftValue,
				IsOnline:   true,
				GiftName:   GiftData.GiftName,
			}
			line.GiftLine = append(line.GiftLine, lineTemp)
		}

		// 按礼物价值降序排序
		sort.SliceStable(line.GiftLine, func(i, j int) bool {
			return line.GiftLine[i].GiftPrice > line.GiftLine[j].GiftPrice
		})

		// 重建索引确保一致性
		line.GiftIndex = make(map[string]int)
		for i, item := range line.GiftLine {
			line.GiftIndex[item.OpenID] = i + 1
		}

		// 发送更新到WS并保存状态
		if len(line.GiftLine) > 0 && line.GiftIndex[GiftData.OpenID] > 0 {
			SendLineToWs(Line{}, line.GiftLine[line.GiftIndex[GiftData.OpenID]-1], GiftLineType)
		}
		SetLine(line)
	}

	return nil
}

var (
	AccessSecret        = "你的access_key_id"
	AppID         int64 = 123456789
	AccessKey           = "你的access_key_secred"
	CurrentIdCode string
)

func RoomConnect(IdCode string) (AppClient *live.Client, GameId string, WsClient *basic.WsClient, HeartbeatCloseChan chan bool) {
	slog.Info("开始创建新连接",
		slog.String("当前ID码", IdCode),
		slog.String("调用栈", string(debug.Stack()))) // 新增调用栈信息
	LinkConfig := live.NewConfig(AccessKey, AccessSecret, AppID)
	client := live.NewClient(LinkConfig)
	CurrentIdCode = IdCode // 保存当前 ID 供后续重连使用

	AppStart, err := client.AppStart(IdCode)
	if err != nil {
		slog.Error("应用流程开启失败", err)
		return nil, "", nil, nil
	}

	// 检查AppStart是否为零值
	if reflect.ValueOf(AppStart).IsZero() {
		slog.Error("AppStart返回为零值")
		return nil, "", nil, nil
	}

	// 使用反射检查AnchorInfo是否为零值
	if reflect.ValueOf(AppStart.AnchorInfo).IsZero() {
		slog.Error("AnchorInfo为零值")
		return nil, "", nil, nil
	}

	RoomId = AppStart.AnchorInfo.RoomID

	HeartbeatCloseChan = make(chan bool, 1)
	NewHeartbeat(client, AppStart.GameInfo.GameID, HeartbeatCloseChan)

	dispatcherHandleMap := basic.DispatcherHandleMap{
		proto.OperationMessage: messageHandle,
	}

	// 修改后的onCloseCallback回调函数
	onCloseCallback := func(wcs *basic.WsClient, startResp basic.StartResp, closeType int) {

		slog.Info("连接关闭事件触发",
			slog.String("当前GameId", GameId),
			slog.String("房间ID", strconv.Itoa(RoomId)),
			slog.String("ID码", CurrentIdCode)) // 新增关闭时关键参数日志

		// 异步延迟重连逻辑
		go func() {
			// 记录旧GameId用于对比
			previousGameId := GameId
			retryInterval := time.Millisecond * 500 // 修改点1：初始间隔从1秒改为500毫秒

			// 修改点1：将重试次数从4次减少到3次
			for i := 0; i < 3; i++ {
				time.Sleep(retryInterval)

				slog.Info("重试连接中...",
					slog.Int("尝试次数", i+1),
					slog.String("旧GameId", previousGameId),
					slog.String("当前ID码", CurrentIdCode))

				// 创建新连接
				newClient, newGameId, newWsClient, newHeartbeatChan := RoomConnect(IdCode)
				if newClient != nil {
					lineMu.Lock()
					// 新增旧心跳通道关闭逻辑
					if HeartbeatCloseChan != nil {
						close(HeartbeatCloseChan) // 关闭旧心跳通道
					}
					// 添加重连验证日志
					slog.Info("重连参数验证",
						slog.String("旧GameId", previousGameId),
						slog.String("新GameId", newGameId),
						slog.String("房间ID", strconv.Itoa(RoomId)),
						slog.String("ID码", CurrentIdCode))

					// 安全更新全局变量
					AppClient = newClient
					GameId = newGameId
					WsClient = newWsClient
					HeartbeatCloseChan = newHeartbeatChan
					lineMu.Unlock()

					slog.Info("重连成功",
						slog.String("新GameId", newGameId),
						slog.Bool("房间ID一致", RoomId == AppStart.AnchorInfo.RoomID)) // 新增一致性检查
					return
				}
				// 修改点2：优化退避策略系数为1.4
				retryInterval = time.Duration(float64(retryInterval) * 1.4)
			}

			// 修改点3：更新错误日志提示次数
			slog.Error("重连失败，达到最大尝试次数（3次）",
				slog.String("最后尝试的ID码", CurrentIdCode),
				slog.String("房间ID", strconv.Itoa(RoomId)))
		}()
	}

	wsClient, err := basic.StartWebsocket(AppStart, dispatcherHandleMap, onCloseCallback, logger)
	if err != nil {
		slog.Error("WebSocket启动失败", err)
		return nil, "", nil, nil
	}

	return client, AppStart.GameInfo.GameID, wsClient, HeartbeatCloseChan
}

var KeyWordMatchMap = make(map[string]bool)

func KeyWordMatchInit(keyWord string) {
	reg := regexp.MustCompile(`[^.,!！；：’"'"?？;:，。、-]+`)
	matches := reg.FindAllString(keyWord, -1)
	for _, match := range matches {
		KeyWordMatchMap[match] = true
	}
}

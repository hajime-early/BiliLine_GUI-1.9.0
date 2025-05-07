package main

import (
	"fmt"
	"regexp"
	"sort"
	"sync"

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
	AccessSecret        = "AccessSecret"
	AppID         int64 = 123456789
	AccessKey           = "AccessKey"
	CurrentIdCode string
)

func RoomConnect(IdCode string) (AppClient *live.Client, GameId string, WsClient *basic.WsClient, HeartbeatCloseChan chan bool) {
	//	初始化应用连接信息配置，自编译请申明以下3个值
	LinkConfig := live.NewConfig(AccessKey, AccessSecret, AppID)

	//	创建Api连接实例
	client := live.NewClient(LinkConfig)
	//	开始身份码认证流程

	AppStart, err := client.AppStart(IdCode)
	RoomId = AppStart.AnchorInfo.RoomID
	if err != nil {
		slog.Error("应用流程开启失败", err)
		return nil, "", nil, nil
	}
	// 开启心跳
	HeartbeatCloseChan = make(chan bool, 1)
	NewHeartbeat(client, AppStart.GameInfo.GameID, HeartbeatCloseChan)

	dispatcherHandleMap := basic.DispatcherHandleMap{
		proto.OperationMessage: messageHandle,
	}
	onCloseCallback := func(wcs *basic.WsClient, startResp basic.StartResp, closeType int) {
		slog.Info("WebsocketClient onClose", startResp)
		// 注意检查关闭类型, 避免无限重连
		if closeType == live.CloseReceivedShutdownMessage || closeType == live.CloseAuthFailed {
			slog.Info("WebsocketClient exit")
			return
		}
		err := wcs.Reconnection(startResp)
		if err != nil {
			slog.Error("Reconnection fail", err)
		}
	}
	// 一键开启websocket
	wsClient, err := basic.StartWebsocket(AppStart, dispatcherHandleMap, onCloseCallback, logger)
	if err != nil {
		panic(err)
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

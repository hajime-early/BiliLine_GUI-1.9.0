package main

import (
	"strings"
	"time"

	"github.com/vtb-link/bianka/proto"
)

func ResponseQueCtrl(DmParsed *proto.CmdDanmuData) {
	// 音乐点歌功能（保持不变）
	if globalConfiguration.EnableMusicServer {
		if strings.HasPrefix(DmParsed.Msg, "点歌 ") {
			SendMusicServer("search", DmParsed.Msg[7:])
		}
	}
	SendDmToWs(DmParsed)

	// 取消排队指令（保持不变）
	if DmParsed.Msg == "取消排队" {
		DeleteLine(DmParsed.OpenID)
		return
	}

	// 寻址指令（保持不变）
	if DmParsed.Msg == "我在哪" {
		SendWhereToWs(DmParsed.OpenID)
		return
	}

	// 仅礼物模式（保持不变）
	if globalConfiguration.IsOnlyGift {
		return
	}

	// 关键词匹配（保持不变）
	if !KeyWordMatchMap[DmParsed.Msg] {
		return
	}

	openID := DmParsed.OpenID

	// 检查是否已在队列中（保持不变）
	if line.GuardIndex[openID] != 0 || line.GiftIndex[openID] != 0 || line.CommonIndex[openID] != 0 {
		return
	}

	//暂停排队功能
	if paused {
		return
	}

	// 特殊用户处理
	_, ok := SpecialUserList[openID]
	switch {
	case ok: // 特殊用户
		UserStruct := SpecialUserList[openID]
		if UserStruct.EndTime < time.Now().Unix() {
			delete(SpecialUserList, openID)
			globalConfiguration.SpecialUserList = SpecialUserList
			SetConfig(globalConfiguration)
			return
		}

		lineTemp := Line{
			OpenID:     openID,
			UserName:   DmParsed.Uname,
			Avatar:     DmParsed.UFace,
			PrintColor: globalConfiguration.GuardPrintColor,
			IsOnline:   true, // 默认设置为在线状态
		}
		line.GuardLine = append(line.GuardLine, lineTemp)
		line.GuardIndex[openID] = len(line.GuardLine)
		SendLineToWs(lineTemp, GiftLine{}, GuardLineType)
		SetLine(line)

		// case DmParsed.GuardLevel <= 3 && DmParsed.GuardLevel != 0: // 舰长/提督
		// 	lineTemp := Line{
		// 		OpenID:     openID,
		// 		UserName:   DmParsed.Uname,
		// 		Avatar:     DmParsed.UFace,
		// 		PrintColor: globalConfiguration.GuardPrintColor,
		// 		IsOnline:   true, // 默认设置为在线状态
		// 	}

	case len(line.CommonLine) < globalConfiguration.MaxLineCount: // 普通用户
		lineTemp := Line{
			OpenID:     openID,
			UserName:   DmParsed.Uname,
			Avatar:     DmParsed.UFace,
			PrintColor: globalConfiguration.CommonPrintColor,
			IsOnline:   true, // 默认设置为在线状态
		}
		line.CommonLine = append(line.CommonLine, lineTemp)
		line.CommonIndex[openID] = len(line.CommonLine)
		SendLineToWs(lineTemp, GiftLine{}, CommonLineType)
		SetLine(line)
	}
}

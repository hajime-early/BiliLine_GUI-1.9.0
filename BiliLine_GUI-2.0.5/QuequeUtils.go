package main

import (
	"fmt"
	"strings"

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
	if line.GiftIndex[openID] != 0 || line.CommonIndex[openID] != 0 {
		fmt.Printf("已在列表中 %v \n", DmParsed.Uname)
		return
	}

	//暂停排队功能
	if paused {
		fmt.Printf("已暂停排队\n")
		return
	}

	// case DmParsed.GuardLevel <= 3 && DmParsed.GuardLevel != 0: // 舰长/提督
	// 	lineTemp := Line{
	// 		OpenID:     openID,
	// 		UserName:   DmParsed.Uname,
	// 		Avatar:     DmParsed.UFace,
	// 		PrintColor: globalConfiguration.GuardPrintColor,
	// 		IsOnline:   true, // 默认设置为在线状态
	// 	}

	if len(line.CommonLine) >= globalConfiguration.MaxLineCount {
		fmt.Printf("已达到最大排队人数，当前普通列表人数：%v 最大普通列表人数：%v \n", len(line.CommonLine), globalConfiguration.MaxLineCount)
		return
	}

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
	fmt.Printf("已添加进排队列表\n")
	SetLine(line)

}

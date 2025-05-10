package main

import (
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var (
	LineBoxItem = make(map[string]*fyne.Container)
	mu          sync.Mutex
	paused      bool = false
)

func MakeCtrlUI() *fyne.Container {

	// 修改1：将vbox包裹在滚动容器中，并设置初始尺寸
	vbox := container.NewVBox()
	scrollContainer := container.NewVScroll(vbox)
	scrollContainer.SetMinSize(fyne.NewSize(600, 800))

	// 修改2：添加底部按钮容器
	bottomButtons := container.NewHBox(
		layout.NewSpacer(),
		widget.NewButton("清空列表", func() {
			vbox.RemoveAll()
			for k := range LineBoxItem {
				DeleteLine(k)
				delete(LineBoxItem, k)
			}
		}),
		widget.NewButton("暂停排队", func() {
			mu.Lock()
			paused = !paused
			mu.Unlock()
		}),
		layout.NewSpacer(),
	)

	var (
		GiftLength   int
		CommonLength int
	)

	// 修改3：创建包含滚动条和底部按钮的主容器
	mainContainer := container.NewBorder(nil, bottomButtons, nil, nil, scrollContainer)

	go func() {
		for {
			OldLine := line
			if len(OldLine.GiftLine) != GiftLength || len(OldLine.CommonLine) != CommonLength {
				vbox.RemoveAll()

				// 修改4：添加带状态显示的文本和完整按钮功能
				for i, i2 := range OldLine.GiftLine {
					LineTemp := i2
					mu.Lock()
					// 获取用户当前状态
					userStatus := LineTemp.IsOnline
					var statusText *canvas.Text // 新增状态文本对象
					nameText := canvas.NewText(LineTemp.UserName, LineTemp.PrintColor.ToRGBA())
					if !userStatus {
						statusText = canvas.NewText("（不在）", color.White)
					} else {
						statusText = canvas.NewText("", color.White) // 空文本保持布局
					}
					nameStatusBox := container.NewHBox(nameText, statusText) // 用户名和状态保持水平布局

					// 新增礼物信息行（黄色字体）[4,8](@ref)
					giftInfo := canvas.NewText(
						fmt.Sprintf("礼物名：%s，累计礼物电池：%.2f", LineTemp.GiftName, LineTemp.GiftPrice),
						color.RGBA{R: 255, G: 255, B: 0, A: 255}) // 黄色字体
					infoColumn := container.NewVBox(nameStatusBox, giftInfo) // 垂直排列基本信息

					LineBoxItem[LineTemp.OpenID] = container.NewHBox(
						canvas.NewText(fmt.Sprintf("%d.", i+1), nil),
						infoColumn, // 替换原有的nameStatusBox
						layout.NewSpacer(),
						widget.NewButton("离场", func() {

							// 切换状态并更新显示
							userStatus := LineTemp.IsOnline
							userStatus = !userStatus
							LineTemp.IsOnline = userStatus
							UpdateUserStatus(LineTemp.OpenID, LineTemp.IsOnline)
							mu.Lock()
							if container, exists := LineBoxItem[LineTemp.OpenID]; exists {

								infoColumn := container.Objects[1].(*fyne.Container)
								nameStatusBox := infoColumn.Objects[0].(*fyne.Container)
								statusText := nameStatusBox.Objects[1].(*canvas.Text)

								// 更新按钮文本和颜色
								btn := container.Objects[3].(*widget.Button)
								if userStatus {
									btn.SetText("离场")
									fmt.Printf("容器结构：%+v\n", container.Objects)
									btn.Importance = widget.LowImportance
								} else {
									btn.SetText("在场")
									fmt.Printf("容器结构：%+v\n", container.Objects)
									btn.Importance = widget.HighImportance
								}
								btn.Refresh() // 新增此行
								if userStatus {
									statusText.Text = ""
								} else {
									statusText.Text = "（不在）"
								}
								statusText.Refresh()
							}
							mu.Unlock()
						}),
						widget.NewButton("删除", func() {
							DeleteLine(LineTemp.OpenID)
							mu.Lock()
							delete(LineBoxItem, LineTemp.OpenID)
							mu.Unlock()
							vbox.Remove(LineBoxItem[LineTemp.OpenID])
							vbox.Refresh()
						}),
					)
					mu.Unlock()
					vbox.Add(LineBoxItem[LineTemp.OpenID])
				}

				// 修改6：CommonLine部分同步添加状态显示和完整功能
				if len(OldLine.CommonLine) != 0 {
					for i, i2 := range OldLine.CommonLine {
						LineTemp := i2
						mu.Lock()
						userStatus := LineTemp.IsOnline
						var statusText *canvas.Text
						nameText := canvas.NewText(LineTemp.UserName, LineTemp.PrintColor.ToRGBA())
						if !userStatus {
							statusText = canvas.NewText("（不在）", color.White)
						} else {
							statusText = canvas.NewText("", color.White)
						}

						LineBoxItem[LineTemp.OpenID] = container.NewHBox(
							canvas.NewText(fmt.Sprintf("%d.", i+1), nil),
							container.NewHBox(
								nameText,
								statusText,
							),
							layout.NewSpacer(),
							widget.NewButton("离场", func() {
								// 同GiftLine的状态切换逻辑（添加相同修改）
								userStatus := LineTemp.IsOnline
								userStatus = !userStatus
								LineTemp.IsOnline = userStatus
								UpdateUserStatus(LineTemp.OpenID, LineTemp.IsOnline)
								mu.Lock()
								if container, exists := LineBoxItem[LineTemp.OpenID]; exists {
									nameContainer := container.Objects[1].(*fyne.Container)
									statusText := nameContainer.Objects[1].(*canvas.Text)

									btn := container.Objects[3].(*widget.Button)
									if userStatus {
										btn.SetText("离场")
										fmt.Printf("容器结构：%+v\n", container.Objects)
										btn.Importance = widget.LowImportance
									} else {
										btn.SetText("在场")
										fmt.Printf("容器结构：%+v\n", container.Objects)
										btn.Importance = widget.HighImportance
									}
									btn.Refresh() // 新增此行
									if userStatus {
										statusText.Text = ""
									} else {
										statusText.Text = "（不在）"
									}
									statusText.Refresh()
								}
								mu.Unlock()
							}),
							widget.NewButton("删除", func() {
								DeleteLine(LineTemp.OpenID)
								mu.Lock()
								delete(LineBoxItem, LineTemp.OpenID)
								mu.Unlock()
								vbox.Remove(LineBoxItem[LineTemp.OpenID])
								vbox.Refresh()
							}),
						)
						mu.Unlock()
						vbox.Add(LineBoxItem[LineTemp.OpenID])
					}
				}
				GiftLength = len(OldLine.GiftLine)
				CommonLength = len(OldLine.CommonLine)
				vbox.Refresh()
			}
			time.Sleep(1 * time.Second)
		}
	}()
	return mainContainer // 返回包含所有元素的主容器
}

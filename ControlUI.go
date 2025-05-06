package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/exp/slog"
)

var (
	LineBoxItem   sync.Map
	mu            sync.RWMutex
	vbox          *fyne.Container
	scroll        *container.Scroll
	lastLineHash  uint64
	refreshMutex  sync.Mutex
	initializedUI bool
	currentWindow fyne.Window
	closeChan     = make(chan struct{})
	refreshFlag   uint32
	paused        bool = false
	pauseBtn      *widget.Button
	testBtn       *widget.Button
)

func computeLineHash() uint64 {
	lineMu.RLock()
	defer lineMu.RUnlock()

	var hash uint64
	for _, item := range line.GuardLine {
		onlineStatus := uint64(0)
		if item.IsOnline {
			onlineStatus = 1
		}
		hash += uint64(len(item.UserName)) + onlineStatus
	}
	for _, item := range line.GiftLine {
		onlineStatus := uint64(0)
		if item.IsOnline {
			onlineStatus = 1
		}
		hash += uint64(len(item.UserName)) + uint64(item.GiftPrice) + onlineStatus
	}
	for _, item := range line.CommonLine {
		onlineStatus := uint64(0)
		if item.IsOnline {
			onlineStatus = 1
		}
		hash += uint64(len(item.UserName)) + onlineStatus
	}
	return hash
}

// 修改safeDeleteUser函数增加更安全的UI操作
func safeDeleteUser(openID string) {
	mu.Lock()
	defer mu.Unlock()

	// 增强防御性检查
	if openID == "" || vbox == nil {
		slog.Error("无效的删除请求", slog.String("OpenID", openID), slog.Any("vbox", vbox != nil))
		return
	}

	if container, exists := LineBoxItem.Load(openID); exists {
		// 使用DoAndWait确保同步完成UI操作
		fyne.DoAndWait(func() {
			// 增加容器有效性检查
			if container == nil || vbox == nil || vbox.Objects == nil {
				slog.Warn("尝试删除无效的UI容器")
				return
			}

			// 先隐藏再移除避免渲染问题
			if cont, ok := container.(*fyne.Container); ok {
				cont.Hide()
				vbox.Remove(cont)
			}
		})

		// 立即从映射中删除
		LineBoxItem.Delete(openID)

		// 添加节流控制
		time.Sleep(100 * time.Millisecond)
	}

	// 使用带panic保护的协程
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("删除协程发生panic",
					slog.Any("recover", r),
					slog.String("stack", string(debug.Stack())))
			}
		}()

		lineMu.Lock()
		defer lineMu.Unlock()
		if err := DeleteLine(openID); err != nil {
			slog.Error("安全删除失败", err, slog.String("OpenID", openID))
		}
	}()
}

func MakeCtrlUI(w fyne.Window) fyne.CanvasObject {
	currentWindow = w

	SpecialUserList = make(map[string]SpecialUserStruct)
	if globalConfiguration.SpecialUserList != nil {
		SpecialUserList = globalConfiguration.SpecialUserList
	}

	if !initializedUI {
		vbox = container.NewVBox()
		scroll = container.NewScroll(vbox)
		w.Resize(fyne.NewSize(600, 800))
		initializedUI = true
	}

	refreshUI := func() {
		if !atomic.CompareAndSwapUint32(&refreshFlag, 0, 1) {
			return
		}
		defer atomic.StoreUint32(&refreshFlag, 0)

		refreshMutex.Lock()
		defer refreshMutex.Unlock()

		lineMu.RLock()
		currentLine := line
		lineMu.RUnlock()

		fyne.Do(func() {
			if vbox != nil {
				vbox.RemoveAll()
				LineBoxItem.Range(func(key, value interface{}) bool {
					LineBoxItem.Delete(key)
					return true
				})
			}
		})

		totalCounter := 1

		// 1. GuardLine处理
		for idx := range currentLine.GuardLine {
			lineTemp := &currentLine.GuardLine[idx]

			numLabel := widget.NewLabel(fmt.Sprintf("%d.", totalCounter))
			numLabel.TextStyle.Bold = true
			totalCounter++

			statusLabel := widget.NewLabel("")
			updateStatus := func() {
				text := ""
				if !lineTemp.IsOnline {
					text = "(不在)"
				}
				statusLabel.SetText(text)
				statusLabel.Refresh()
			}
			updateStatus()

			stateBtn := widget.NewButton("", func() {})
			updateButton := func() {
				fyne.Do(func() {
					if lineTemp.IsOnline {
						stateBtn.SetText("离场")
						stateBtn.Importance = widget.HighImportance
					} else {
						stateBtn.SetText("在场")
						stateBtn.Importance = widget.MediumImportance
					}
					stateBtn.Refresh()
				})
			}
			updateButton()

			stateBtn.OnTapped = func() {
				lineMu.Lock()
				lineTemp.IsOnline = !lineTemp.IsOnline
				for i := range line.GuardLine {
					if line.GuardLine[i].OpenID == lineTemp.OpenID {
						line.GuardLine[i].IsOnline = lineTemp.IsOnline
						break
					}
				}
				SetLine(line)
				lineMu.Unlock()

				msg := map[string]interface{}{
					"OpMessage": 3,
					"Data": map[string]interface{}{
						"OpenID":   lineTemp.OpenID,
						"IsOnline": lineTemp.IsOnline,
					},
				}
				if msgBytes, err := json.Marshal(msg); err == nil {
					QueueChatChan <- msgBytes
				}

				// 修复：使用fyne.Do包装UI更新
				fyne.Do(func() {
					updateStatus()
					updateButton()
				})
			}

			deleteBtn := widget.NewButton("删除", func() {
				safeDeleteUser(lineTemp.OpenID)
			})

			container := container.NewHBox(
				canvas.NewText("⚓ ", color.RGBA{255, 215, 0, 255}),
				numLabel,
				container.NewHBox(
					canvas.NewText(lineTemp.UserName, lineTemp.PrintColor.ToRGBA()),
					statusLabel,
				),
				layout.NewSpacer(),
				container.NewHBox(
					stateBtn,
					deleteBtn,
				),
			)

			LineBoxItem.Store(lineTemp.OpenID, container)

			fyne.Do(func() {
				if vbox != nil {
					vbox.Add(container)
				}
			})
		}

		// 2. GiftLine处理
		for idx := range currentLine.GiftLine {
			lineTemp := &currentLine.GiftLine[idx]

			numLabel := widget.NewLabel(fmt.Sprintf("%d.", totalCounter))
			numLabel.TextStyle.Bold = true
			totalCounter++

			statusLabel := widget.NewLabel("")
			updateStatus := func() {
				text := ""
				if !lineTemp.IsOnline {
					text = "(不在)"
				}
				statusLabel.SetText(text)
				statusLabel.Refresh()
			}
			updateStatus()

			giftInfoLabel := widget.NewLabel(fmt.Sprintf("礼物名：\"%s\"，累计礼物电池：\"%.2f\"",
				lineTemp.GiftName, lineTemp.GiftPrice))
			giftInfoLabel.TextStyle.Italic = true
			giftInfoLabel.TextStyle.Monospace = true

			stateBtn := widget.NewButton("", func() {})
			updateButton := func() {
				fyne.Do(func() {
					if lineTemp.IsOnline {
						stateBtn.SetText("离场")
						stateBtn.Importance = widget.HighImportance
					} else {
						stateBtn.SetText("在场")
						stateBtn.Importance = widget.MediumImportance
					}
					stateBtn.Refresh()
				})
			}
			updateButton()

			stateBtn.OnTapped = func() {
				lineMu.Lock()
				lineTemp.IsOnline = !lineTemp.IsOnline
				for i := range line.GiftLine {
					if line.GiftLine[i].OpenID == lineTemp.OpenID {
						line.GiftLine[i].IsOnline = lineTemp.IsOnline
						break
					}
				}
				SetLine(line)
				lineMu.Unlock()

				msg := map[string]interface{}{
					"OpMessage": 3,
					"Data": map[string]interface{}{
						"OpenID":   lineTemp.OpenID,
						"IsOnline": lineTemp.IsOnline,
					},
				}
				if msgBytes, err := json.Marshal(msg); err == nil {
					QueueChatChan <- msgBytes
				}

				// 修复：使用fyne.Do包装UI更新
				fyne.Do(func() {
					updateStatus()
					updateButton()
				})
			}

			giftDeleteBtn := widget.NewButton("删除", func() {
				safeDeleteUser(lineTemp.OpenID)
			})

			container := container.NewHBox(
				canvas.NewText("🎁 ", color.RGBA{255, 0, 0, 255}),
				numLabel,
				container.NewVBox(
					container.NewHBox(
						canvas.NewText(lineTemp.UserName, lineTemp.PrintColor.ToRGBA()),
						statusLabel,
					),
					giftInfoLabel,
				),
				layout.NewSpacer(),
				container.NewHBox(
					stateBtn,
					giftDeleteBtn,
				),
			)

			LineBoxItem.Store(lineTemp.OpenID, container)

			fyne.Do(func() {
				if vbox != nil {
					vbox.Add(container)
				}
			})
		}

		// 3. CommonLine处理
		if len(currentLine.CommonLine) != 0 {
			for idx := range currentLine.CommonLine {
				lineTemp := &currentLine.CommonLine[idx]

				numLabel := widget.NewLabel(fmt.Sprintf("%d.", totalCounter))
				numLabel.TextStyle.Bold = true
				totalCounter++

				statusLabel := widget.NewLabel("")
				updateStatus := func() {
					text := ""
					if !lineTemp.IsOnline {
						text = "(不在)"
					}
					statusLabel.SetText(text)
					statusLabel.Refresh()
				}
				updateStatus()

				stateBtn := widget.NewButton("", func() {})
				updateButton := func() {
					fyne.Do(func() {
						if lineTemp.IsOnline {
							stateBtn.SetText("离场")
							stateBtn.Importance = widget.HighImportance
						} else {
							stateBtn.SetText("在场")
							stateBtn.Importance = widget.MediumImportance
						}
						stateBtn.Refresh()
					})
				}
				updateButton()

				stateBtn.OnTapped = func() {
					lineMu.Lock()
					lineTemp.IsOnline = !lineTemp.IsOnline
					for i := range line.CommonLine {
						if line.CommonLine[i].OpenID == lineTemp.OpenID {
							line.CommonLine[i].IsOnline = lineTemp.IsOnline
							break
						}
					}
					SetLine(line)
					lineMu.Unlock()

					msg := map[string]interface{}{
						"OpMessage": 3,
						"Data": map[string]interface{}{
							"OpenID":   lineTemp.OpenID,
							"IsOnline": lineTemp.IsOnline,
						},
					}
					if msgBytes, err := json.Marshal(msg); err == nil {
						QueueChatChan <- msgBytes
					}

					// 修复：使用fyne.Do包装UI更新
					fyne.Do(func() {
						updateStatus()
						updateButton()
					})
				}

				commonDeleteBtn := widget.NewButton("删除", func() {
					safeDeleteUser(lineTemp.OpenID)
				})

				container := container.NewHBox(
					canvas.NewText("💬 ", color.RGBA{0, 150, 255, 255}),
					numLabel,
					container.NewHBox(
						canvas.NewText(lineTemp.UserName, lineTemp.PrintColor.ToRGBA()),
						statusLabel,
					),
					layout.NewSpacer(),
					container.NewHBox(
						stateBtn,
						commonDeleteBtn,
					),
				)

				LineBoxItem.Store(lineTemp.OpenID, container)

				fyne.Do(func() {
					if vbox != nil {
						vbox.Add(container)
					}
				})
			}
		}

		clearAllBtn := widget.NewButton("清空列表", func() {
			mu.Lock()
			defer mu.Unlock()

			fyne.Do(func() {
				if vbox != nil {
					vbox.RemoveAll()
					LineBoxItem.Range(func(key, value interface{}) bool {
						LineBoxItem.Delete(key)
						return true
					})
				}
			})

			go func() {
				lineMu.Lock()
				defer lineMu.Unlock()
				line.GuardLine = []Line{}
				line.GiftLine = []GiftLine{}
				line.CommonLine = []Line{}
				SetLine(line)
			}()
		})
		clearAllBtn.Importance = widget.DangerImportance

		// 初始化暂停按钮
		pauseBtn = widget.NewButton("暂停排队", func() {
			paused = !paused
			if paused {
				pauseBtn.SetText("恢复排队")
			} else {
				pauseBtn.SetText("暂停排队")
			}
		})
		pauseBtn.Importance = widget.WarningImportance

		buttonRow := container.NewHBox()
		buttonRow.Add(pauseBtn)
		buttonRow.Add(layout.NewSpacer())
		buttonRow.Add(clearAllBtn)

		// 替换最后的返回部分
		fyne.Do(func() {
			if vbox != nil {
				vbox.Add(container.NewCenter(buttonRow))
				vbox.Refresh()
			}
			if scroll != nil {
				scroll.Refresh()
			}
		})
	}

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				currentHash := computeLineHash()
				if currentHash != lastLineHash {
					refreshUI()
					lastLineHash = currentHash
				}
			case <-closeChan:
				return
			}
		}
	}()

	return scroll
}

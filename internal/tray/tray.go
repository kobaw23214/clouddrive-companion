package tray

import (
	"context"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getlantern/systray"
	wailsRunTime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppContext 定义 App 需要提供的接口
type AppContext interface {
	GetContext() context.Context
}

var (
	startOnce sync.Once // 托盘只启动一次
	quitOnce  sync.Once // systray.Quit 只发一次，避免重复关闭消息循环
	exitOnce  sync.Once // OnTrayExit 可能被 wndProc 触发多次，exitedCh 只关一次
	started   atomic.Bool
	quitting  atomic.Bool // 是否是我们主动要求退出，用于识别消息泵异常结束
	exitedCh  = make(chan struct{})
)

// Start 启动系统托盘。进程内只生效一次，重复调用直接忽略——
// 本地 WebView2 启动失败回退系统版本时会二次触发 Wails 的 OnStartup，
// 没有这个保护会起出两个托盘图标、两份菜单。
func Start(app AppContext, icon []byte) {
	startOnce.Do(func() {
		started.Store(true)
		go func() {
			// 关键：把这条 goroutine 钉死在一个独占的 OS 线程上，并且永不 Unlock。
			//
			// systray 只在自己的 init() 里调过一次 runtime.LockOSThread()，锁到的是
			// main goroutine；我们是在这里另起 goroutine 跑 systray.Run，所以不受它保护。
			// systray.Run 会先 CreateWindowEx 建一个隐藏窗口并 Shell_NotifyIcon(NIM_ADD)
			// 注册托盘图标，然后进入 GetMessageW 循环。Win32 的消息队列是线程绑定的：
			// 托盘图标的鼠标消息只会投递到"创建该窗口的那个线程"的队列里。
			//
			// 如果不锁线程，每次 GetMessage 系统调用返回后 Go 调度器都可能把这条 goroutine
			// 迁到另一个 OS 线程（进程里还有 HTTP 服务、上传协程、sqlite、tracer 队列在抢 P，
			// 运行越久、负载越高越容易发生）。一旦迁走，之后的 GetMessage 就在一个永远收不到
			// 托盘消息的线程队列上空等：图标还在（NIM_DELETE 没发过），但左键右键全无反应。
			// 这正是"常驻一段时间后右键点不动"的成因。锁定线程后窗口、消息泵、wndProc
			// 始终在同一线程，消息不会再丢。
			runtime.LockOSThread()

			systray.Run(func() {
				OnTrayReady(app, icon)
			}, func() {
				OnTrayExit()
			})

			// systray.Run 返回意味着消息泵结束了。正常退出走 Quit()，此时 quitting 为 true。
			// 否则说明 GetMessage 返回了 -1（窗口句柄失效等）而 systray 只是静默 return，
			// 托盘图标会变成一个"点不动的幽灵"。这里留日志，便于用户反馈时对账。
			if !quitting.Load() {
				log.Println("[tray] 警告：托盘消息循环意外结束（GetMessage 失败或窗口被销毁），托盘图标将不再响应点击")
			}
		}()
	})
}

// Quit 退出托盘并等待图标清理完成（最多等 300ms）。
//
// systray.Quit() 只是给托盘的消息循环发退出信号，真正移除图标的 NIM_DELETE
// 发生在该消息循环里。如果调完 Quit 立刻 os.Exit，清理可能来不及执行，
// 托盘区会留下一个"点不动"的幽灵图标（用户右键它没有任何反应），
// 直到鼠标划过才被 Explorer 探测回收。所以这里等消息循环退出后再返回。
// 未启动过托盘时直接返回。
func Quit() {
	if !started.Load() {
		return
	}
	quitting.Store(true)
	quitOnce.Do(func() {
		systray.Quit()
	})
	select {
	case <-exitedCh:
	case <-time.After(300 * time.Millisecond):
	}
}

// OnTrayReady 托盘图标准备就绪时的回调
func OnTrayReady(app AppContext, icon []byte) {
	// 设置托盘图标
	systray.SetIcon(icon)
	systray.SetTitle("上传客户端伴侣")
	systray.SetTooltip("上传客户端伴侣 - 文件上传助手")

	// 创建菜单项
	mShow := systray.AddMenuItem("显示上传面板", "显示上传任务面板")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出应用程序")

	// 监听菜单点击事件。
	// systray 投递点击是非阻塞的（select + default），这条循环要是卡在上一次点击的
	// 处理里，后面的点击会被直接丢弃、表现为"菜单点了没反应"，所以每次点击都丢给
	// 单独的 goroutine 处理，循环本身永远保持可读。
	go func() {
		for {
			select {
			case <-mShow.ClickedCh:
				log.Println("托盘菜单：请求显示上传面板")
				go func() {
					if ctx := app.GetContext(); ctx != nil {
						wailsRunTime.EventsEmit(ctx, "tray:showDashboard")
					}
				}()

			case <-mQuit.ClickedCh:
				log.Println("托盘菜单：请求退出应用")
				go func() {
					if ctx := app.GetContext(); ctx != nil {
						// 退出统一走 App.Quit()：先收托盘图标、flush 追踪/历史，再退进程
						wailsRunTime.EventsEmit(ctx, "tray:quit")
					} else {
						// wails 上下文还没就绪，事件没人接收。
						// 不能让"退出"点了没反应，这里自行收尾后直接退进程。
						log.Println("wails 上下文未就绪，托盘直接退出进程")
						Quit()
						os.Exit(0)
					}
				}()
			}
		}
	}()

	log.Println("系统托盘图标已创建")
}

// OnTrayExit 托盘退出时的回调。
//
// 这个回调由 systray 的 wndProc 触发（WM_DESTROY / WM_ENDSESSION）。注销登录或
// 关机时两条消息都可能到达，回调会被调用两次，直接 close 已关闭的 channel 会 panic
// 在 Win32 回调里、把进程带崩，所以用 once 保护。
func OnTrayExit() {
	exitOnce.Do(func() {
		log.Println("系统托盘已退出")
		close(exitedCh)
	})
}

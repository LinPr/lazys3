# 重点

Model-View-Update


Msg 的本质：万物皆可为消息
一个“事件”就是一个发生了的事情，而这个事情所携带的信息是多种多样的。用 interface{} 来承载

自定义 Msg：异步通信的桥梁bubbletea 真正强大的地方在于它允许我们定义自己的 Msg 类型。这通常用于异步操作完成后的通信。
发起了一个网络请求去获取数据。当请求成功或失败后，我们需要把结果通知给 Update 函数。这时，我们就可以定义自己的 Msg

在 Update 函数里，我们就可以通过 switch msg.(type) 来分别处理这些自定义消息，从而更新 Model的状态（比如，更新列表数据或显示错误提示）


如何执行网络请求、文件读写这类耗时操作（在编程中称为副作用 Side Effects）而又不阻塞 UI


Cmd 不是动作，而是对动作的描述。 当你的 Update 函数返回一个 Cmd 时，你并不是在立即执行那个函数。你只是在告诉 bubbletea 运行时：“嘿，麻烦你在后台帮我执行这个函数（副作用）。

bubbletea 运行时负责执行。 bubbletea 会在一个单独的 goroutine 中执行你返回的 Cmd 函数，因此它完全不会阻塞主循环和 UI 渲染。

Cmd 的结果是一个 Msg。 当 Cmd 函数执行完毕后，它的返回值（一个 Msg）会被 bubbletea 捕获，并作为新的事件发送给你下一轮的 Update 函数。

这个 Update -> Cmd -> (runtime executes) -> Msg -> Update 的流程，形成了一个优雅、安全、可测试的异步闭环。


func fetchQuoteCmd() tea.Cmd {
    returnfunc() tea.Msg {
        // 这里是真正的副作用
        resp, err := http.Get("http://api.quotable.io/random")
        if err != nil {
            // 如果出错，返回一个错误消息
            return errMsg{Err: err}
        }
        defer resp.Body.Close()
        
        // ... (省略解析 JSON 的代码) ...
        var result map[string]interface{}
        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
            return errMsg{Err: err}
        }

        // 如果成功，返回一个包含名言的消息
        return quoteMsg{Quote: result["content"].(string)}
    }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    // ...
    // 当收到名言消息时
    case quoteMsg:
        m.quote = msg.Quote
        m.err = nil
        return m, nil// 不需要新的命令
    // 当收到错误消息时
    case errMsg:
        m.err = msg
        return m, tea.Quit // 出错了，退出程序
    }
    // ...
}

bubbletea 也提供了一些方便的内置 Cmd。其中最常用的是 tea.Tick，它会以指定的时间间隔，持续地发送一个 tickMsg。这对于实现加载动画、定时器、时钟等功能非常有用。


回顾核心： 我们理解了 Msg 是应用内所有事件的统一载体，
而 Cmd 则是安全执行副作用、连接外部世界的桥梁。


tea.Msg 是 interface{}： 任何类型都可以是消息，自定义消息是实现异步通信的关键。
tea.Cmd 是 func() Msg： 它是一个对副作用的“描述”，由运行时在后台执行，执行结果会包装成 Msg 送回 Update
。
tea.Batch： 可以将多个 Cmd 合并为一个，方便在 Init 或 Update 中一次性返回。
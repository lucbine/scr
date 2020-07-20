// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package time

// Sleep pauses the current goroutine for at least the duration d.   【Sleep 方法用来暂停当前gorouting 一段时间】
// A negative or zero duration causes Sleep to return immediately.   【如果duration 是0或者负数 Sleep立马返回】
func Sleep(d Duration)

// Interface to timers implemented in package runtime.【这是计时器再runtime包里的接口实现】
// Must be in sync with ../runtime/time.go:/^type timer
type runtimeTimer struct { //创建定时器的协程并不负责计时，而是把任务交给系统协程，系统协程统一处理所有的定时器
	pp uintptr // timer 所在的 P 的指针
	// 当时间为 when 时，唤醒 timer，当时间为 when+period, ... (period > 0)
	// 时，均在 timer goroutine 中调用 f(arg, now)，从而 f 必须具有良好的行为（不会阻塞）
	when     int64                      //当前计时器被唤醒的时间  仅仅只是将事件触发的 持续时间 转换为 int64
	period   int64                      //两次被唤醒的间隔
	f        func(interface{}, uintptr) // NOTE: must not be closure	//底层回调函数  每当计时器被唤醒都会调度的函数
	arg      interface{}                //参数  计时器被唤醒时调用f传入的参数
	seq      uintptr
	nextwhen int64  //计时器处于 timerModifiedXX 状态时，用于设置 when 字段
	status   uint32 //计时器的状态
}

// when is a helper function for setting the 'when' field of a runtimeTimer. 【when是一个帮助函数，用于设置runtimeTimer的“when”字段。】
// It returns what the time will be, in nanoseconds, Duration d in the future. 【它返回未来的持续时间，纳秒格式】
// If d is negative, it is ignored. If the returned value would be less than 【如果参数d 是负数，函数不起作用，如果返回值是小于0 因为数据溢出 最大是int64】
// zero because of an overflow, MaxInt64 is returned.
func when(d Duration) int64 {
	if d <= 0 {
		return runtimeNano()
	}
	t := runtimeNano() + int64(d)
	if t < 0 {
		t = 1<<63 - 1 // math.MaxInt64
	}
	return t
}

func startTimer(*runtimeTimer)        //【开始计时器】
func stopTimer(*runtimeTimer) bool    //停止计时器
func resetTimer(*runtimeTimer, int64) //重置计时器

// The Timer type represents a single event.
// When the Timer expires, the current time will be sent on C,
// unless the Timer was created by AfterFunc. 【一个计时器代表一个单独的事件，当计时器到期，当前的时间将被发送到channel C 中 ,除非Timer 是被AfterFunc 创建的。】
// A Timer must be created with NewTimer or AfterFunc.【一个计时器 必须被NewTimer 或者 AfterFunc 创建】

// 当计时器失效时，失效的时间就会被发送给计时器持有的 Channel，订阅 Channel 的 Goroutine 会收到计时器失效的时间。
type Timer struct {
	C <-chan Time
	r runtimeTimer
}

// Stop prevents the Timer from firing.   【Stop 方法用来停止计时器的启动】
// It returns true if the call stops the timer, false if the timer has already
// expired or been stopped.    【如果正确停止了计时器返回true,如果计时器已经过期或者已经停止返回false】
// Stop does not close the channel, to prevent a read from the channel succeeding
// incorrectly. 【Stop方法并不是关闭channel ,而是阻止channel的不正确的读取】
//
// To ensure the channel is empty after a call to Stop, check the
// return value and drain the channel.   【为确保调用Stop方法后channel是空的，需要检查返回值并清空channel】
// For example, assuming the program has not received from t.C already:  【例如，假如程序还没有从channel中获得数据】
//
// 	if !t.Stop() {
// 		<-t.C
// 	}
//
// This cannot be done concurrent to other receives from the Timer's
// channel or other calls to the Timer's Stop method.
//
// For a timer created with AfterFunc(d, f), if t.Stop returns false, then the timer
// has already expired and the function f has been started in its own goroutine;
// Stop does not wait for f to complete before returning.
// If the caller needs to know whether f is completed, it must coordinate
// with f explicitly.
func (t *Timer) Stop() bool {
	if t.r.f == nil {
		panic("time: Stop called on uninitialized Timer")
	}
	return stopTimer(&t.r)
}

// NewTimer creates a new Timer that will send
// the current time on its channel after at least duration d.
func NewTimer(d Duration) *Timer {
	// 注意，这里的channel是带缓冲的，保证了业务层如果不接收这个channel，底层的
	// 桶协程不会因为发送channel而被阻塞
	c := make(chan Time, 1)
	t := &Timer{
		C: c,
		r: runtimeTimer{ //实现runtime接口
			when: when(d),  //
			f:    sendTime, // 向底层timer传入sendTime回调函数
			arg:  c,
		},
	}
	startTimer(&t.r) //addtimer 将 timer 添加到创建 timer 的 P 上。
	return t
}

// Reset changes the timer to expire after duration d.
// It returns true if the timer had been active, false if the timer had
// expired or been stopped.
//
// Reset should be invoked only on stopped or expired timers with drained channels.
// If a program has already received a value from t.C, the timer is known
// to have expired and the channel drained, so t.Reset can be used directly.
// If a program has not yet received a value from t.C, however,
// the timer must be stopped and—if Stop reports that the timer expired
// before being stopped—the channel explicitly drained:
//
// 	if !t.Stop() {
// 		<-t.C
// 	}
// 	t.Reset(d)
//
// This should not be done concurrent to other receives from the Timer's
// channel.
//
// Note that it is not possible to use Reset's return value correctly, as there
// is a race condition between draining the channel and the new timer expiring.
// Reset should always be invoked on stopped or expired channels, as described above.
// The return value exists to preserve compatibility with existing programs.
func (t *Timer) Reset(d Duration) bool {
	if t.r.f == nil {
		panic("time: Reset called on uninitialized Timer")
	}
	w := when(d)
	active := stopTimer(&t.r)
	resetTimer(&t.r, w)
	return active
}

// 将底层的超时回调转化为channel发送，并写入了当前时间
func sendTime(c interface{}, seq uintptr) {
	// Non-blocking send of time on c.
	// Used in NewTimer, it cannot block anyway (buffer).
	// Used in NewTicker, dropping sends on the floor is
	// the desired behavior when the reader gets behind,
	// because the sends are periodic.
	select {
	case c.(chan Time) <- Now():
	default:
	}
}

// After waits for the duration to elapse and then sends the current time   【After 函数 等待duration时间段后消失，并且发送当前的时间 到返回的channel 中】
// on the returned channel.
// It is equivalent to NewTimer(d).C.		【它和NewTimer(d).C 等价】
// The underlying Timer is not recovered by the garbage collector
// until the timer fires. If efficiency is a concern, use NewTimer
// instead and call Timer.Stop if the timer is no longer needed.
func After(d Duration) <-chan Time { //// After就是匿名Timer
	return NewTimer(d).C
}

// AfterFunc waits for the duration to elapse and then calls f
// in its own goroutine. It returns a Timer that can
// be used to cancel the call using its Stop method.
func AfterFunc(d Duration, f func()) *Timer {
	t := &Timer{
		r: runtimeTimer{
			when: when(d),
			f:    goFunc,
			arg:  f,
		},
	}
	startTimer(&t.r)
	return t
}

func goFunc(arg interface{}, seq uintptr) {
	go arg.(func())()
}

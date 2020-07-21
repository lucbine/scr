/*
@Time : 2020/7/20 2:04 下午
@Author : lucbine
@File : time_test.go
*/
package scr

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type WaitGroupWrapper struct {
	wg sync.WaitGroup
}

func (w *WaitGroupWrapper) Wrap(cb func()) {
	w.wg.Add(1)
	go func() {
		cb()
		w.wg.Done()
	}()
}

func (w *WaitGroupWrapper) Wait() {
	w.Wait()
}

func (w *WaitGroupWrapper) WaitTimeout(timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		w.wg.Wait()
	}()

	select {
	case <-c:
		return false //completed normally
	case <-time.After(timeout):
		return true
	}
}

func TestNewTimer_test1(t *testing.T) {
	tr := time.NewTimer(2 * time.Second)
	<-tr.C
	//fmt.Println(tr.Stop())
	fmt.Println("Timer 1 expired")

}

func TestNewTimer_test2(t *testing.T) {
	tr := time.NewTimer(2 * time.Second)

	go func() {
		<-tr.C
	}()

	fmt.Println(tr.Stop())
	fmt.Println("Timer 2 expired")

}

func TestWait_test3(t *testing.T) {

	//设置时区
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	//创建时间
	t1 := time.Date(2018, 7, 7, 12, 12, 12, 500000000, location)
	fmt.Println(t1)

	//字符串转化时间
	t2, err := time.Parse("2006-01-02 15:04:05", "2018-07-07 09:10:05")

	fmt.Println(t2)

	//将字符串转成时间，需要传入时区
	t3, err := time.ParseInLocation("20060102", "20180707", time.UTC)

	fmt.Println(t3)

	t4 := time.Now()

	fmt.Println(t4)

	fmt.Println(t4.Format("2006-01-02 15:04:05"))
	fmt.Println(t4.Format("02/01/2006 15:04:05"))
	fmt.Println(t4.Format("2006-01-02"))
	fmt.Println(t4.Format("15:04:05"))
	fmt.Println(t4.Format("January 2,2006"))

	//获得世界统一时间
	t5 := t4.UTC()
	fmt.Println(t5)

	//获得本地时间
	t6 := t5.Local()

	fmt.Println(t6)

	//获得指定时区的时间
	t7 := t6.In(location)

	fmt.Println(t7)

	//获得unix时间戳
	timestamp := t7.Unix()
	fmt.Println(timestamp)

	//获得unix时间戳 单位：纳秒 常用与作为rand的随机种子
	timestamp = t7.UnixNano()
	fmt.Println(timestamp)

	//判断两个时间是否相等，会判断时区等信息 不同时区也可以用此进行比较
	fmt.Println(t7.Equal(t6))

	//t4 是否在t3 之前
	fmt.Println(t4.Before(t3))

	//判断t4 是否在t3 之后
	fmt.Println(t4.After(t3))

	//返回时间的年月日
	y, m, d := t7.Date()

	fmt.Println("y:", y, "m:", m, "d:", d)

	//返回时间的时分秒
	h, min, s := t7.Clock()

	fmt.Printf("时：%d，分：%d，秒：%d\n", h, min, s) // 时：21，分：5，秒：41

	//计算两个时间之差
	dur := t4.Sub(t7)
	fmt.Println(dur)

	//将时间戳转成Time对象
	t8 := time.Unix(0, timestamp)
	fmt.Println(t8.Format("2006-01-02 15:04:05"))

}

func TestTimer_test4(t *testing.T) {
	timer := time.NewTimer(1 * time.Second)
	fmt.Println(<-timer.C)

	//也可以配合select 使用
	timer = time.NewTimer(1 * time.Second)

	select {
	case <-timer.C:

		fmt.Println("执行")

	}

	//用timer 做定时器
	timer = time.NewTimer(1 * time.Second)

	for {
		select {
		case <-timer.C:
			fmt.Println("Timer 定时器")
			timer.Reset(1 * time.Second)

		}
	}
}

func TestTimer_test5(t *testing.T) {
	//开启一个新协程，在指定时间后执行给定的函数，所以测试时候，需要将主协程休眠几秒后才能看到执行结果
	time.AfterFunc(1*time.Second, func() {
		fmt.Println("after func...")
	})
	//当前协程休眠置顶时间
	time.Sleep(2 * time.Second)

	//置顶时间后 想通道C发送时间
	tt := <-time.After(2 * time.Second)

	fmt.Println(tt)

}

func TestTick_test6t(t *testing.T) {

	tc := time.Tick(time.Second)
	for {
		select {
		case ab := <-tc:
			fmt.Println("time:", ab)
		}
	}

	ticker := time.NewTicker(time.Second)

	//用ticker 实现定时器
	for {
		select {
		case <-ticker.C:
			fmt.Println("Ticker ...")
		}
	}
}

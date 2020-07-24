/*
@Time : 2020/7/20 2:04 下午
@Author : lucbine
@File : time_test.go
*/
package cron

import (
	"fmt"
	"testing"
	"time"
)

type MyJob struct {
	Name string
	Id   int
}

func (mj *MyJob) Run() {
	fmt.Println("heheheheheheheheheh")
}

func TestCron_test1(t *testing.T) {
	c := New(WithSeconds()) //精确到秒

	spec := "*/1 * * * * ?"

	c.AddFunc(spec, func() {
		fmt.Println("hello")
	})

	c.AddFunc(spec, func() {
		fmt.Println("lucbine")
	})

	c.Start()

	time.Sleep(10 * time.Second)
}

func TestCron_test2(t *testing.T) {
	c := New(WithSeconds())

	spec := "*/1 * * * * ?"

	var job = &MyJob{
		Id:   10,
		Name: "lucbine",
	}

	c.AddJob(spec, job)

	c.Start()
	time.Sleep(10 * time.Second)

}

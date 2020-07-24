/*
@Time : 2020/7/20 2:04 下午
@Author : lucbine
@File : time_test.go
*/
package scr

import (
	"testing"

	"cron"
)

func TestCron_test1(t *testing.T) {
	c := cron.New(cron.WithSeconds())
}

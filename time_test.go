/*
@Time : 2020/7/20 2:04 下午
@Author : lucbine
@File : time_test.go
*/
package scr

import (
	"testing"
	"time"
)

func TestNewTimer_test(t *testing.T) {
	tr := time.NewTimer(10 * time.Second)
}

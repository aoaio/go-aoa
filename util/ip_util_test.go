package util

import "testing"
import "fmt"

func TestGetLocalIP(t *testing.T) {
	fmt.Println(GetLocalIP())
}

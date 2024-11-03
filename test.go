package main

import (
	"fmt"
	"github.com/metacubex/mihomo/listener"
)

func main() {
	m, _ := listener.ParseListener(map[string]any{})
	fmt.Println(m)
}

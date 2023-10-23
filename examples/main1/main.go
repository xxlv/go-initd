package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	sigs := make(chan os.Signal)
	signal.Notify(sigs, syscall.SIGTERM)
	go func() {
		for {
			x := <-sigs
			println("accept", x.String())
			time.Sleep(3 * time.Second)
			return
		}

	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		println("hello main1")
		fmt.Fprintf(w, "Hello, World Main1!")
	})

	fmt.Println("Listen on 9080 ...")

	if err := http.ListenAndServe(":9080", nil); err != nil {
		panic(err)
	}

}

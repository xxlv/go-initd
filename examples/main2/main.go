package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		println("hello main2" + time.Now().GoString())
		fmt.Fprintf(w, "Hello, World Main2!")
	})

	fmt.Println("Listen on 9082...")
	if err := http.ListenAndServe(":9082", nil); err != nil {
		panic(err)
	}
}

package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// Response 响应结构体
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
	Data any    `json:"data,omitempty"`
}

func response(w http.ResponseWriter, code int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(Response{
		Code: code,
		Msg:  msg,
		Data: data,
	}); err != nil {
		log.Fatalln("json response error:", err)
	}
}

func success(w http.ResponseWriter, data any) {
	response(w, 200, "", data)
}

func fail(w http.ResponseWriter, code int, msg string) {
	response(w, code, msg, nil)
}

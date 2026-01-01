package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"testing"
)

type User struct {
	UserId string `json:"user_id"`
	Asset  string `json:"asset"`
}

func TestDrawJosn(t *testing.T) {
	var users = []User{}
	for i := 0; i < 1000; i++ {
		users = append(users, User{
			UserId: fmt.Sprintf("%d", i),
			Asset:  "BTC",
		})
	}
	marshal, _ := json.Marshal(users)
	ioutil.WriteFile("users.json", marshal, 0644)
}

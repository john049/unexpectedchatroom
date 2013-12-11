package main

import (
	"bufio"
	"container/list" //包container里有链表list
	"fmt"
	"io"
	"net/http"
)

var connid int
var conns *list.List

func main() {
	connid = 0                                        //初始化ID
	conns = list.New()                                //创建链表
	http.Handle("/chatroom", Handler(ChatroomServer)) //HTTP包处理函数，调用服务端函数
	http.HandleFunc("/", Client)                      //调用客户端处理函数
	err := http.ListenAndServe(":6611", nil)          //监听6611端口
	if err != nil {
		panic("ListenAndServe: " + err.Error()) //错误处理
	}
}

func ChatroomServer(was *Conn) {
	defer was.Close()
	connid++ //生成连接数ID号
	id := connid
	fmt.Printf("connection id: %d\n", id) //输出ID号
	item := conns.PushBack(was)
	defer conns.Remove(item)
	name := fmt.Sprintf("user%d", id)                        //用户ID
	SendMessage(nil, fmt.Sprintf("welcome %s join\n", name)) //输出欢迎
	r := bufio.NewReader(was)
	for {
		data, err := r.ReadBytes('\n') //数据data从这个传入缓冲区
		if err != nil {
			fmt.Printf("disconnected id: %d\n", id) //断开连接
			SendMessage(item, fmt.Sprintf("%s offline\n", name))
			break
		}
		ch := make(chan string)
		go fmt.Printf("%s: %s", name, data) //并发的输出用户信息
		go getString(ch, name, data)        //定义ch为goroutine之间通信，传递信息。
		go SendMessage(item, <-ch)          //接收ch传递进来的信息，作为参数传入
	}

}

func SendMessage(self *list.Element, data string) { //发送消息函数
	// for _, item := range conns {
	for item := conns.Front(); item != nil; item = item.Next() { //遍历链表
		was, ok := item.Value.(*Conn)
		if !ok {
			panic("item not *websocket.Conn")
		}

		if item == self { //如果是自己发的消息则继续等待，直到发现别的用户发来消息
			continue
		}

		io.WriteString(was, data)
	}
}

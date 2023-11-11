package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
)

const SERVER_PORT int = 8999

var HELLO_MSG string = "欢迎加入%v, 可以用/nickname设置昵称\n"

var removeChan chan *client
var dataChan chan msg

type client struct {
	id   int
	conn *net.TCPConn
	nick string
}

type msg struct {
	id   int
	data []byte
}

type chatServer interface {
	CreateClient(conn *net.TCPConn)
	CloseClient(c *client)
	SpreadMessage(msg msg)
	genId() int
}

type serverState struct {
	numclients int
	clients    map[int]*client
	maxId      int
	mu         sync.Mutex
}

func (chat *serverState) CreateClient(conn *net.TCPConn) {
	chat.mu.Lock()
	defer chat.mu.Unlock()
	nick := fmt.Sprintf("user:%v", rand.Int())
	conn.SetKeepAlive(true)
	conn.SetNoDelay(true)
	conn.SetReadBuffer(1024)
	conn.SetWriteBuffer(1024)
	_, err := conn.Write([]byte(fmt.Sprintf(HELLO_MSG, nick)))
	if err != nil {
		log.Fatal(err)
	}
	cl := &client{
		id:   chat.genId(),
		conn: conn,
		nick: nick,
	}

	chat.clients[cl.id] = cl
	chat.numclients++
	fmt.Printf("%v connected, there are %d clients in the room\n", conn.LocalAddr().String(), chat.numclients)
	go listenMessage(cl)
}

func (chat *serverState) CloseClient(c *client) {
	chat.mu.Lock()
	defer chat.mu.Unlock()
	fmt.Printf("%v disconnected\n", c.nick)
	defer c.conn.Close()
	delete(chat.clients, c.id)
	chat.numclients--
}

func (chat *serverState) genId() int {
	// 因为之前可能加了锁
	ok := chat.mu.TryLock()
	if ok {
		defer chat.mu.Unlock()
	}
	chat.maxId++
	return chat.maxId
}

func (chat *serverState) SpreadMessage(msg msg) {
	for _, cl := range chat.clients {
		if cl.id == msg.id {
			continue
		}
		_, err := cl.conn.Write(msg.data)
		if err != nil {
			removeChan <- cl
		}
	}
}

func listenMessage(cl *client) {
	buffer := make([]byte, 1024)
	for {
		n, err := cl.conn.Read(buffer)
		if err != nil {
			removeChan <- cl
			break
		} else {
			dataChan <- msg{id: cl.id, data: buffer[:n]}
		}
	}

}

func main() {
	chat := &serverState{
		numclients: 0,
		clients:    make(map[int]*client),
	}
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", SERVER_PORT))
	if err != nil {
		log.Fatal(err)
	}
	tcpl, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer tcpl.Close()
	fmt.Printf("Listen: %v\n", tcpl.Addr().String())

	connChan := make(chan *net.TCPConn)
	dataChan = make(chan msg)
	removeChan = make(chan *client)

	// 监听是否有新的连接到来
	go func() {
		for {
			conn, err := tcpl.AcceptTCP()
			if err != nil {
				log.Fatal(err)
			}
			connChan <- conn
		}
	}()

	for {
		select {
		case conn := <-connChan:
			go chat.CreateClient(conn)
		case m := <-dataChan:
			go chat.SpreadMessage(m)
		case cl := <-removeChan:
			go chat.CloseClient(cl)
		}
	}
}

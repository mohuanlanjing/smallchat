package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
)

const SERVER_PORT int = 8999

var HELLO_MSG string = "欢迎加入%v, 可以用/nickname+空格+新昵称设置新昵称\n如/nickname newnick\n"
var connChan chan *net.TCPConn
var removeChan chan chatClient
var dataChan chan message
var noticeChan chan string

type client struct {
	id   int
	conn *net.TCPConn
	nick string
}

type message struct {
	client_id int
	user      string
	data      []byte
}

type server struct {
	ls         *net.TCPListener
	numclients int
	clients    map[int]chatClient
	maxId      int
	mu         sync.Mutex
}

type chatServer interface {
	Init(port int)
	CreateClient(conn *net.TCPConn)
	CloseClient(c chatClient)
	SpreadMessage(m message)
	SendNotice(msg []byte)
	SendMessageTo(c chatClient, msg message)
	genId() int
	getClient(id int) chatClient
	ListenForever(connChan chan *net.TCPConn)
	Close()
}

type chatClient interface {
	Init(id int, conn *net.TCPConn)
	changeNick(nick string)
	GetId() int
	GetNick() string
	Close()
	GetMessage([]byte) (int, error)
	receiveMessage(msg string) error
	ListenForever()
}

func (cl *client) changeNick(nick string) {
	cl.nick = string(nick)
}

func (cl *client) GetNick() string {
	return cl.nick
}

func (cl *client) Init(id int, conn *net.TCPConn) {
	conn.SetKeepAlive(true)
	conn.SetNoDelay(true)
	conn.SetReadBuffer(1024)
	conn.SetWriteBuffer(1024)
	cl.id = id
	cl.conn = conn
	cl.nick = fmt.Sprintf("Guest%d", rand.Intn(1000000))
}

func (cl *client) receiveMessage(msg string) error {
	_, err := cl.conn.Write([]byte(msg))
	return err
}

func (cl *client) AcceptCommand(command string, args []string) error {
	switch command {
	case "nickname":
		cl.nick = strings.Join(args, "_")
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	return nil
}

func (cl *client) GetMessage(buffer []byte) (int, error) {
	return cl.conn.Read(buffer)
}

func (cl *client) Close() {
	cl.conn.Close()
}

func (cl *client) GetId() int {
	return cl.id
}

func (cl *client) ListenForever() {
	buffer := make([]byte, 1024)
	for {
		n, err := cl.GetMessage(buffer)
		if err != nil {
			removeChan <- cl
			break
		}
		switch buffer[0] {
		case '/':
			fields := strings.Fields(string(buffer[1:n]))
			cl.AcceptCommand(fields[0], fields[1:])
		default:
			dataChan <- message{client_id: cl.id, user: cl.nick, data: buffer[:n]}
		}

	}
}

func (chat *server) CreateClient(conn *net.TCPConn) {
	chat.mu.Lock()
	defer chat.mu.Unlock()
	cl := &client{}
	cl.Init(chat.genId(), conn)
	hello_msg := message{
		client_id: cl.id,
		user:      cl.nick,
		data:      []byte(fmt.Sprintf(HELLO_MSG, cl.nick)),
	}
	err := chat.SendMessageTo(cl, hello_msg)
	if err != nil {
		return
	}
	chat.clients[cl.GetId()] = cl
	chat.numclients++
	fmt.Printf("%v connected, there are %d clients in the room\n", conn.LocalAddr().String(), chat.numclients)
	go cl.ListenForever()
}

func (chat *server) CloseClient(c chatClient) {
	chat.mu.Lock()
	defer chat.mu.Unlock()
	defer c.Close()
	msg := fmt.Sprintf("%v disconnected\n", c.GetNick())
	fmt.Println(msg)
	noticeChan <- msg
	delete(chat.clients, c.GetId())
	chat.numclients--
}

func (chat *server) genId() int {
	// 因为之前可能加了锁
	ok := chat.mu.TryLock()
	if ok {
		defer chat.mu.Unlock()
	}
	chat.maxId++
	return chat.maxId
}

func (chat *server) Init(port int) {
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}
	tcpl, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Listen: %v\n", tcpl.Addr().String())
	chat.ls = tcpl

}

func (chat *server) SendMessageTo(cl chatClient, msg message) error {
	m := fmt.Sprintf("%v: %v\n", msg.user, string(msg.data))
	return cl.receiveMessage(m)
}

func (chat *server) SendNotice(msg string) {
	for _, cl := range chat.clients {
		err := cl.receiveMessage(msg)
		if err != nil {
			removeChan <- cl
		}
	}
}

func (chat *server) Close() {
	chat.ls.Close()
}

func (chat *server) ListenForever(connChan chan *net.TCPConn) {
	go func() {
		for {
			conn, err := chat.ls.AcceptTCP()
			if err != nil {
				log.Fatal(err)
			}
			connChan <- conn
		}
	}()
}

func (chat *server) SpreadMessage(msg message) {
	for _, cl := range chat.clients {
		if cl.GetId() == msg.client_id {
			continue
		}
		err := chat.SendMessageTo(cl, msg)
		if err != nil {
			removeChan <- cl
		}
	}
}

func main() {
	chat := &server{
		numclients: 0,
		clients:    make(map[int]chatClient),
	}

	connChan = make(chan *net.TCPConn)
	dataChan = make(chan message)
	removeChan = make(chan chatClient)
	noticeChan = make(chan string)

	chat.Init(SERVER_PORT)
	defer chat.Close()
	go chat.ListenForever(connChan)

	for {
		select {
		case conn := <-connChan:
			go chat.CreateClient(conn)
		case m := <-dataChan:
			go chat.SpreadMessage(m)
		case cl := <-removeChan:
			go chat.CloseClient(cl)
		case m := <-noticeChan:
			go chat.SendNotice(m)
		}
	}
}

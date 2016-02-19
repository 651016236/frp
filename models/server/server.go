package server

import (
	"container/list"
	"sync"

	"github.com/fatedier/frp/models/consts"
	"github.com/fatedier/frp/utils/conn"
	"github.com/fatedier/frp/utils/log"
)

type ProxyServer struct {
	Name        string
	Passwd      string
	BindAddr    string
	ListenPort  int64
	Status      int64
	CliConnChan chan *conn.Conn // get client conns from control goroutine

	listener     *conn.Listener // accept new connection from remote users
	ctlMsgChan   chan int64     // every time accept a new user conn, put "1" to the channel
	userConnList *list.List     // store user conns
	mutex        sync.Mutex
}

func (p *ProxyServer) Init() {
	p.Status = consts.Idle
	p.CliConnChan = make(chan *conn.Conn)
	p.ctlMsgChan = make(chan int64)
	p.userConnList = list.New()
}

func (p *ProxyServer) Lock() {
	p.mutex.Lock()
}

func (p *ProxyServer) Unlock() {
	p.mutex.Unlock()
}

// start listening for user conns
func (p *ProxyServer) Start() (err error) {
	p.Init()
	p.listener, err = conn.Listen(p.BindAddr, p.ListenPort)
	if err != nil {
		return err
	}

	p.Status = consts.Working

	// start a goroutine for listener
	go func() {
		for {
			// block
			// if listener is closed, get nil
			c := p.listener.GetConn()
			if c == nil {
				log.Info("ProxyName [%s], listener is closed", p.Name)
				return
			}
			log.Debug("ProxyName [%s], get one new user conn [%s]", p.Name, c.GetRemoteAddr())

			// insert into list
			p.Lock()
			if p.Status != consts.Working {
				log.Debug("ProxyName [%s] is not working, new user conn close", p.Name)
				c.Close()
				p.Unlock()
				return
			}
			p.userConnList.PushBack(c)
			p.Unlock()

			// put msg to control conn
			p.ctlMsgChan <- 1
		}
	}()

	// start another goroutine for join two conns from client and user
	go func() {
		for {
			cliConn, ok := <-p.CliConnChan
			if !ok {
				return
			}

			p.Lock()
			element := p.userConnList.Front()

			var userConn *conn.Conn
			if element != nil {
				userConn = element.Value.(*conn.Conn)
				p.userConnList.Remove(element)
			} else {
				cliConn.Close()
				p.Unlock()
				continue
			}
			p.Unlock()

			// msg will transfer to another without modifying
			// l means local, r means remote
			log.Debug("Join two conns, (l[%s] r[%s]) (l[%s] r[%s])", cliConn.GetLocalAddr(), cliConn.GetRemoteAddr(),
				userConn.GetLocalAddr(), userConn.GetRemoteAddr())
			go conn.Join(cliConn, userConn)
		}
	}()

	return nil
}

func (p *ProxyServer) Close() {
	p.Lock()
	p.Status = consts.Idle
	p.listener.Close()
	close(p.ctlMsgChan)
	close(p.CliConnChan)
	p.userConnList = list.New()
	p.Unlock()
}

func (p *ProxyServer) WaitUserConn() (closeFlag bool) {
	closeFlag = false

	_, ok := <-p.ctlMsgChan
	if !ok {
		closeFlag = true
	}
	return
}

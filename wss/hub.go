// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wss

import (
	"log"
	"strconv"
	"encoding/json"
)

type Broadcast struct {
	Id string
	Username string
	Body string
	Tag string
	Room string
}

// hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[string]*Client

	// Inbound messages from the clients.
	broadcast chan Broadcast

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	authorization chan *Client

	admins map[*Client]bool

	rooms map[string]map[*Client]bool

	message chan Broadcast

	self chan Broadcast

	join chan Broadcast

	leave chan Broadcast

	queue chan Broadcast

	queues map[string]bool
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan Broadcast),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[string]*Client),
		authorization:   make(chan *Client),
		admins: make(map[*Client]bool),
		rooms: make(map[string]map[*Client]bool),
		message:  make(chan Broadcast),
		self:  make(chan Broadcast),
		join:  make(chan Broadcast),
		leave:  make(chan Broadcast),
		queue:  make(chan Broadcast),
		queues: make(map[string]bool),
	}
}

func (h *Hub) Run() {
	log.Println("hub running")
	for {
		select {
			case client := <-h.register:
				log.Println("hub::register")
				h.clients[client.id] = client
				h.rooms[client.id] = make(map[*Client]bool)
				h.rooms[client.id][client] = true
				client.send <- Broadcast{client.id, client.username, "Successfully connected", "handshake" , client.id}
				h.broadcastGeneralInfo()
				h.broadcastToAdmin()
			case client := <-h.unregister:
				log.Println("hub::unregister")
				if _, ok := h.clients[client.id]; ok {
					h.broadcastClientLeaveRoom(client)
					delete(h.clients, client.id)
					delete(h.rooms, client.id)
					delete(h.admins, client)
					delete(h.queues, client.id)
					for room,_ := range h.rooms {
						delete(h.rooms[room],client)
					}
					//close(client.send)
					h.broadcastGeneralInfo()
				}
				h.broadcastToAdmin()
			case client := <-h.authorization:
				log.Println("hub::authorization")
				if client.level == "ADMIN" {
					h.admins[client] = true
				} else {
					if h.admins[client] {
						delete(h.admins, client)
						h.broadcastClientLeaveRoom(client)
						for room,_ := range h.rooms {
							if room != client.id {
								delete(h.rooms[room],client)
							}
						}
					}
				}
				for _,c := range h.clients {
					c.send <- Broadcast{client.id, client.username, strconv.Itoa(len(h.admins)), "admins" , client.id}
				}
				delete(h.queues, client.id)
				h.broadcastToAdmin()
			case broadcast := <-h.broadcast:
				log.Println("hub::broadcast")
				for _,client := range h.clients {
					select {
					case client.send <- broadcast:
					default:
						//close(client.send)
						delete(h.clients, client.id)
						delete(h.admins, client)
					}
				}
			case message := <-h.message:
				log.Println("hub::message")
				if h.rooms[message.Room][h.clients[message.Id]] {
					for client,_ := range h.rooms[message.Room] {
						if message.Id != client.id {
							select {
							case client.send <- message:
							default:
								//close(client.send)
								delete(h.clients, client.id)
								delete(h.admins, client)
							}
						}
					}
				}
			case self := <-h.self:
				log.Println("hub::self")
				if h.clients[self.Id] != nil {
					h.clients[self.Id].send <- self
				}
			case join := <-h.join:
				log.Println("hub::join")
				if(h.admins[h.clients[join.Id]]) {
					if(join.Body != join.Id) {
						delete(h.queues, join.Body)
						h.rooms[join.Body][h.clients[join.Id]] = true
						h.broadcastToAdmin()
						h.clients[join.Id].send <- Broadcast{"0", "SYSTEM", "SUCCESSFULLY JOIN","message", "0"}
						for client,_ := range h.rooms[join.Body] {
							if join.Id != client.id {
								client.send <- Broadcast{"0", "SYSTEM", join.Username + " JOINED", "message", "0"}
							}
						}
					}
				} else {
					h.clients[join.Id].send <- Broadcast{"0", "SYSTEM", "FORBIDDEN:ADMIN ONLY", "message", "0"}
				}
			case leave := <-h.leave:
				log.Println("hub::leave")
				if(h.admins[h.clients[leave.Id]]) {
					if(leave.Body != leave.Id) {
						if h.rooms[leave.Body][h.clients[leave.Id]] {
							delete(h.rooms[leave.Body],h.clients[leave.Id])
							h.broadcastToAdmin()
							h.clients[leave.Id].send <- Broadcast{"0", "SYSTEM", "SUCCESSFULLY LEAVE","message", "0"}
							for client,_ := range h.rooms[leave.Body] {
								if leave.Id != client.id {
									client.send <- Broadcast{"0", "SYSTEM", leave.Username + " LEAVED", "message", "0"}
								}
							}
						}
					}
				} else {
					h.clients[leave.Id].send <- Broadcast{"0", "SYSTEM", "FORBIDDEN:ADMIN ONLY", "message", "0"}
				}
			case queue := <-h.queue:
				log.Println("hub::queue")
				if(h.admins[h.clients[queue.Id]]) {
					h.clients[queue.Id].send <- Broadcast{"0", "SYSTEM", "FORBIDDEN:GUEST ONLY", "message", "0"}
				} else {
					if h.queues[queue.Id] || len(h.rooms[queue.Id]) > 1 {
						h.clients[queue.Id].send <- Broadcast{"0", "SYSTEM", "ALREADY IN QUEUE", "message", "0"}
					} else {
						h.queues[queue.Id] = true
						h.clients[queue.Id].send <- Broadcast{"0", "SYSTEM", "SUCCESSFULLY QUEUE", "message", "0"}
						h.broadcastToAdmin()
					}
				}
		}
	}
}

func (h *Hub) broadcastClientLeaveRoom(client *Client) {
	for c,_ := range h.rooms[client.id] {
		if client.id != c.id {
			c.send <- Broadcast{"0", "SYSTEM", client.username + " LEAVED", "message", "0"} // if leave their room
		}
	}
	for room,_ := range h.rooms {
		if (h.rooms[room][client]) && room != client.id{
			for c,_ := range h.rooms[room] {
				if client.id != c.id {
					c.send <- Broadcast{"0", "SYSTEM", client.username + " LEAVED", "message", "0"} // if leave other room
				}
			}
		}
	}
}

func (h *Hub) broadcastToAdmin() {
	rooms := make(map[string][]interface{})
	for id,c := range h.rooms {
		for m,_  := range c {
			rooms[id] = append(rooms[id], map[string]interface{}{
				"username" : m.username,
				"level" : m.level,
			})
		}
	}
	r, _ := json.Marshal(rooms)
	var queues []string
	for id,_ := range h.queues {
		queues = append(queues,id)
	}
	q, _ := json.Marshal(queues)
	for c,_ := range h.admins {
		c.send <- Broadcast{"0", "SYSTEM", string(r), "room_list" , "0"}
		c.send <- Broadcast{"0", "SYSTEM", string(q), "queue_list" , "0"}
	}
}

func (h *Hub) broadcastGeneralInfo() {
	for _,c := range h.clients {
		c.send <- Broadcast{"0", "SYSTEM", strconv.Itoa(len(h.clients)), "online" , "0"}
		c.send <- Broadcast{"0", "SYSTEM", strconv.Itoa(len(h.admins)), "admins" , "0"}
		c.send <- Broadcast{"0", "SYSTEM", strconv.Itoa(len(h.rooms)), "rooms" , "0"}
	}
}

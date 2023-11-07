package brpc

import (
	"github.com/google/uuid"
	"sync"
)

type clientMap[ClientService any] struct {
	clients     map[uuid.UUID]ClientService
	clientsLock sync.RWMutex
}

func (c *clientMap[ClientService]) add(id uuid.UUID, client ClientService) {
	c.clientsLock.Lock()
	defer c.clientsLock.Unlock()
	c.clients[id] = client
}

func (c *clientMap[ClientService]) remove(id uuid.UUID) {
	c.clientsLock.Lock()
	defer c.clientsLock.Unlock()
	delete(c.clients, id)
}

func (c *clientMap[ClientService]) get(id uuid.UUID) (ClientService, bool) {
	c.clientsLock.RLock()
	defer c.clientsLock.RUnlock()
	client, ok := c.clients[id]
	return client, ok
}

func newClientMap[ClientService any]() *clientMap[ClientService] {
	return &clientMap[ClientService]{
		clients: make(map[uuid.UUID]ClientService),
	}
}

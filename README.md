# go-smtp-server

[![GoDoc](https://godoc.org/github.com/nguoianphu/go-smtp-server?status.svg)](https://godoc.org/github.com/nguoianphu/go-smtp-server)
[![Build Status](https://travis-ci.org/nguoianphu/go-smtp-server.svg?branch=master)](https://travis-ci.org/nguoianphu/go-smtp-server)

An ESMTP server library written in Go.

## Features

* ESMTP server implementing [RFC 5321](https://tools.ietf.org/html/rfc5321)
* Support for SMTP AUTH ([RFC 4954](https://tools.ietf.org/html/rfc4954)) and PIPELINING ([RFC 2920](https://tools.ietf.org/html/rfc2920))
* UTF-8 support for subject and message


- username: ```username```
- password: ```password```

## Build and compile

	go get github.com/nguoianphu/go-smtp-server
	cd $GOPATH/src/github.com/nguoianphu/go-smtp-server
	go build github.com/nguoianphu/go-smtp-server/go-smtp-server.go
	./go-smtp-server

## Usage in your golang code

```go
// +build ignore

package main

import (
	"errors"
	"io/ioutil"
	"log"

	smtpserver "github.com/nguoianphu/go-smtp-server"
)

type Backend struct{}

func (bkd *Backend) Login(username, password string) (smtpserver.User, error) {
	if username != "username" || password != "password" {
		return nil, errors.New("Invalid username or password")
	}
	return &User{}, nil
}

type User struct{}

func (u *User) Send(msg *smtpserver.Message) error {
	log.Println("Sending message:", msg)

	if b, err := ioutil.ReadAll(msg.Data); err != nil {
		return err
	} else {
		log.Println("Data:", string(b))
	}
	return nil
}

func (u *User) Logout() error {
	return nil
}

func main() {
	bkd := &Backend{}

	s := smtpserver.New(bkd)

	s.Addr = ":1025"
	s.Domain = "localhost"
	s.MaxIdleSeconds = 300
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	log.Println("Starting server at", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
```

You can use the server manually with `telnet`:
```
$ telnet localhost 25
EHLO localhost
AUTH PLAIN
AHVzZXJuYW1lAHBhc3N3b3Jk
MAIL FROM:<root@nsa.gov>
RCPT TO:<root@gchq.gov.uk>
DATA
Hey <3
.
```

## Licence

MIT

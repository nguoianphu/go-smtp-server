// +build ignore

package go-smtp-server

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

    s.Addr = ":25"
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
package smtp

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Conn struct {
	server    *Server
	helo      string
	User      User
	msg       *Message
	conn      net.Conn
	reader    *bufio.Reader
	writer    *bufio.Writer
	nbrErrors int
}

func newConn(c net.Conn, s *Server) *Conn {
	sc := &Conn{
		server: s,
		conn:   c,
	}

	sc.init()
	return sc
}

func (c *Conn) init() {
	r := io.Reader(c.conn)
	w := io.Writer(c.conn)

	if c.server.Debug != nil {
		r = io.TeeReader(r, c.server.Debug)
		w = io.MultiWriter(w, c.server.Debug)
	}

	c.reader = bufio.NewReader(r)
	c.writer = bufio.NewWriter(w)
}

// Commands are dispatched to the appropriate handler functions.
func (c *Conn) handle(cmd string, arg string) {
	if cmd == "" {
		c.Write("500", "Speak up")
		return
	}

	switch cmd {
	case "SEND", "SOML", "SAML", "EXPN", "HELP", "TURN":
		// These commands are not implemented in any state
		c.Write("502", fmt.Sprintf("%v command not implemented", cmd))
	case "HELO", "EHLO":
		c.handleGreet((cmd == "EHLO"), arg)
	case "MAIL":
		c.handleMail(arg)
	case "RCPT":
		c.handleRcpt(arg)
	case "VRFY":
		c.Write("252", "Cannot VRFY user, but will accept message")
	case "NOOP":
		c.Write("250", "I have sucessfully done nothing")
	case "RSET": // Reset session
		c.reset()
		c.Write("250", "Session reset")
	case "DATA":
		c.handleData(arg)
	case "QUIT":
		c.Write("221", "Goodnight and good luck")
		c.Close()
	case "AUTH":
		c.handleAuth(arg)
	case "STARTTLS":
		c.handleStartTLS()
	default:
		c.Write("500", fmt.Sprintf("Syntax error, %v command unrecognized", cmd))

		c.nbrErrors++
		if c.nbrErrors > 3 {
			c.Write("500", "Too many unrecognized commands")
			c.Close()
		}
	}
}

func (c *Conn) Server() *Server {
	return c.server
}

func (c *Conn) Close() error {
	if c.User != nil {
		c.User.Logout()
	}

	return c.conn.Close()
}

// Check if this connection is encrypted.
func (c *Conn) IsTLS() bool {
	_, ok := c.conn.(*tls.Conn)
	return ok
}

// GREET state -> waiting for HELO
func (c *Conn) handleGreet(enhanced bool, arg string) {
	if !enhanced {
		domain, err := parseHelloArgument(arg)
		if err != nil {
			c.Write("501", "Domain/address argument required for HELO")
			return
		}
		c.helo = domain

		c.Write("250", fmt.Sprintf("Hello %s", domain))
	} else {
		domain, err := parseHelloArgument(arg)
		if err != nil {
			c.Write("501", "Domain/address argument required for EHLO")
			return
		}

		c.helo = domain

		caps := []string{}
		caps = append(caps, c.server.caps...)
		if c.server.TLSConfig != nil && !c.IsTLS() {
			caps = append(caps, "STARTTLS")
		}
		if c.IsTLS() || c.server.AllowInsecureAuth {
			authCap := "AUTH"
			for name, _ := range c.server.auths {
				authCap += " " + name
			}

			caps = append(caps, authCap)
		}
		if c.server.MaxMessageBytes > 0 {
			caps = append(caps, fmt.Sprintf("SIZE %v", c.server.MaxMessageBytes))
		}

		args := []string{"Hello " + domain}
		args = append(args, caps...)
		c.Write("250", args...)
	}
}

// READY state -> waiting for MAIL
func (c *Conn) handleMail(arg string) {
	if c.helo == "" {
		c.Write("502", "Please introduce yourself first.")
		return
	}
	if c.msg == nil {
		c.Write("502", "Please authenticate first.")
		return
	}

	// Match FROM, while accepting '>' as quoted pair and in double quoted strings
	// (?i) makes the regex case insensitive, (?:) is non-grouping sub-match
	re := regexp.MustCompile("(?i)^FROM:\\s*<((?:\\\\>|[^>])+|\"[^\"]+\"@[^>]+)>( [\\w= ]+)?$")
	m := re.FindStringSubmatch(arg)
	if m == nil {
		c.Write("501", "Was expecting MAIL arg syntax of FROM:<address>")
		return
	}

	from := m[1]

	// This is where the Conn may put BODY=8BITMIME, but we already
	// read the DATA as bytes, so it does not effect our processing.
	if m[2] != "" {
		args, err := parseArgs(m[2])
		if err != nil {
			c.Write("501", "Unable to parse MAIL ESMTP parameters")
			return
		}

		if args["SIZE"] != "" {
			size, err := strconv.ParseInt(args["SIZE"], 10, 32)
			if err != nil {
				c.Write("501", "Unable to parse SIZE as an integer")
				return
			}

			if c.server.MaxMessageBytes > 0 && int(size) > c.server.MaxMessageBytes {
				c.Write("552", "Max message size exceeded")
				return
			}
		}
	}

	c.msg.From = from
	c.Write("250", fmt.Sprintf("Roger, accepting mail from <%v>", from))
}

// MAIL state -> waiting for RCPTs followed by DATA
func (c *Conn) handleRcpt(arg string) {
	if c.msg == nil || c.msg.From == "" {
		c.Write("502", "Missing MAIL FROM command.")
		return
	}

	if (len(arg) < 4) || (strings.ToUpper(arg[0:3]) != "TO:") {
		c.Write("501", "Was expecting RCPT arg syntax of TO:<address>")
		return
	}

	// TODO: This trim is probably too forgiving
	recipient := strings.Trim(arg[3:], "<> ")

	if c.server.MaxRecipients > 0 && len(c.msg.To) >= c.server.MaxRecipients {
		c.Write("552", fmt.Sprintf("Maximum limit of %v recipients reached", c.server.MaxRecipients))
		return
	}

	c.msg.To = append(c.msg.To, recipient)
	c.Write("250", fmt.Sprintf("I'll make sure <%v> gets this", recipient))
}

func (c *Conn) handleAuth(arg string) {
	if c.helo == "" {
		c.Write("502", "Please introduce yourself first.")
		return
	}

	if arg == "" {
		c.Write("502", "Missing parameter")
		return
	}

	parts := strings.Fields(arg)
	mechanism := strings.ToUpper(parts[0])

	// Parse client initial response if there is one
	var ir []byte
	if len(parts) > 1 {
		var err error
		ir, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return
		}
	}

	newSasl, ok := c.server.auths[mechanism]
	if !ok {
		c.Write("504", "Unsupported authentication mechanism")
		return
	}

	sasl := newSasl(c)
	scanner := bufio.NewScanner(c.reader)

	response := ir
	for {
		challenge, done, err := sasl.Next(response)
		if err != nil {
			c.Write("454", err.Error())
			return
		}

		if done {
			break
		}

		encoded := ""
		if len(challenge) > 0 {
			encoded = base64.StdEncoding.EncodeToString(challenge)
		}
		c.Write("334", encoded)

		if !scanner.Scan() {
			return
		}

		encoded = scanner.Text()
		if encoded != "" {
			response, err = base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				c.Write("454", "Invalid base64 data")
				return
			}
		}
	}

	if c.User != nil {
		c.Write("235", "Authentication succeeded")

		c.msg = &Message{}
	}
}

func (c *Conn) handleStartTLS() {
	if c.IsTLS() {
		c.Write("502", "Already running in TLS")
		return
	}

	if c.server.TLSConfig == nil {
		c.Write("502", "TLS not supported")
		return
	}

	c.Write("220", "Ready to start TLS")

	// Upgrade to TLS
	var tlsConn *tls.Conn
	tlsConn = tls.Server(c.conn, c.server.TLSConfig)

	if err := tlsConn.Handshake(); err != nil {
		c.Write("550", "Handshake error")
	}

	c.conn = tlsConn
	c.init()

	// Reset envelope as a new EHLO/HELO is required after STARTTLS
	c.reset()
}

// DATA
func (c *Conn) handleData(arg string) {
	if arg != "" {
		c.Write("501", "DATA command should not have any arguments")
		return
	}

	if c.msg == nil || c.msg.From == "" || len(c.msg.To) == 0 {
		c.Write("502", "Missing RCPT TO command.")
		return
	}

	// We have recipients, go to accept data
	c.Write("354", "Go ahead. End your data with <CR><LF>.<CR><LF>")

	c.msg.Data = newDataReader(c)
	if err := c.User.Send(c.msg); err != nil {
		if err, ok := err.(*smtpError); ok {
			c.Write(err.Code, err.Message)
		} else {
			c.Write("554", "Error: transaction failed, blame it on the weather: "+err.Error())
		}
	} else {
		c.Write("250", "Ok: queued")
	}

	c.reset()
}

func (c *Conn) Reject() {
	c.Write("421", "Too busy. Try again later.")
	c.Close()
}

func (c *Conn) greet() {
	c.Write("220", fmt.Sprintf("%v ESMTP Service Ready", c.server.Domain))
}

// Calculate the next read or write deadline based on MaxIdleSeconds.
func (c *Conn) nextDeadline() time.Time {
	if c.server.MaxIdleSeconds == 0 {
		return time.Time{} // No deadline
	}

	return time.Now().Add(time.Duration(c.server.MaxIdleSeconds) * time.Second)
}

func (c *Conn) Write(code string, text ...string) {
	// TODO: error handling

	c.conn.SetDeadline(c.nextDeadline())

	for i := 0; i < len(text)-1; i++ {
		c.writer.Write([]byte(code + "-" + text[i] + "\r\n"))
	}
	c.writer.Write([]byte(code + " " + text[len(text)-1] + "\r\n"))
	c.writer.Flush()
}

// Reads a line of input
func (c *Conn) readLine() (line string, err error) {
	if err = c.conn.SetReadDeadline(c.nextDeadline()); err != nil {
		return "", err
	}

	line, err = c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return line, nil
}

func (c *Conn) reset() {
	if c.User != nil {
		c.User.Logout()
	}

	c.helo = ""
	c.User = nil
	c.msg = nil
}

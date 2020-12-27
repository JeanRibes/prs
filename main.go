package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
)

const MTU = 1400

func e(err error) {
	if err != nil {
		panic(err)
	}
}
func clearbuf(buf []byte) {
	for i := 0; i < len(buf); i++ {
		buf[i] = 0
	}
}
func welcome(conn *net.UDPConn, newport int) (*net.UDPAddr, int) {
	buf := make([]byte, 100)
	for {
		clearbuf(buf)
		_, client0, _ := conn.ReadFromUDP(buf)
		println(string(buf))
		if string(buf)[0:3] == "SYN" {
			println("syn")
			clearbuf(buf)
			println("new port:", newport)
			conn.WriteTo([]byte(fmt.Sprintf("SYN-ACK%04d", newport)), client0)
			_, client1, _ := conn.ReadFromUDP(buf)
			if client0.String() == client1.String() {
				if string(buf)[0:3] == "ACK" {
					return client1, newport
				}
			} else {
				println("mismatch")
				fmt.Printf("%v %v", client1, client0)
			}
		}
	}
}
func getfile(conn *net.UDPConn) *os.File {
	buf := make([]byte, 1000)
	conn.Read(buf)
	filename := strings.Trim(string(buf), "\x00")
	println(filename)
	println(len(filename))
	file, ferr := os.Open(filename)
	e(ferr)
	return file

}
func sendfile(data_conn *net.UDPConn, client *net.UDPAddr, file *os.File) {
	buf := make([]byte, MTU-6)
	seq_num := 1
	for {
		n, _ := file.Read(buf[6:])
		copy(buf[0:6], fmt.Sprintf("%06d", seq_num))
		if n == 0 {
			data_conn.WriteTo([]byte("FIN"), client)
			return
		}

		_, we := data_conn.WriteTo(buf[0:6+n], client) // on envoie pas plus que ce qu'on a lu
		e(we)
		_, re := data_conn.Read(buf)
		e(re)
		seq_num++
		println(seq_num)
		clearbuf(buf)
	}
}
func main() {
	waddr, _ := net.ResolveUDPAddr("udp", "0.0.0.0:5000")
	welcome_conn, _ := net.ListenUDP("udp", waddr)

	randint, re := rand.Int(rand.Reader, big.NewInt(8975))
	e(re)
	newport := int(randint.Int64() + 1024)

	daddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%04d", newport))
	data_conn, _ := net.ListenUDP("udp", daddr)

	client, newport := welcome(welcome_conn, newport) //il faut ouvrir la socket avant car sinon on ne reçoit pas tout

	file := getfile(data_conn)
	println(client.String(), file)
	sendfile(data_conn, client, file)

}

package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

const (
	MTU     = 140
	WSIZE   = 75
	TIMEOUT = time.Millisecond * 10
)
const MAX_DATA = MTU - 6

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
	clearbuf(buf)
	_, re := conn.Read(buf)
	e(re)
	filename := strings.Trim(string(buf), "\x00")
	println(filename)
	println(len(filename))
	file, ferr := os.Open(filename)
	e(ferr)
	return file

}
func parse_ack(s string) (ack int) {
	fmt.Sscanf(s, "%06d", &ack)
	return ack
}

/*
prépare tous les paquets avec le numéro de séquence devant
*/
func prepare_packets(file *os.File) (buffer [][]byte, n int) {
	fi, _ := file.Stat()
	size := fi.Size()
	num_packets := (size / MTU) + 1
	buffer = make([][]byte, num_packets)

	for i := 0; i < len(buffer); i++ {
		if n == 0 && i > 1 {
			panic("erreur de logique")
		}
		buffer[i] = make([]byte, MTU)

		copy(buffer[i][0:6], fmt.Sprintf("%06d", i+1))
		n, _ = file.Read(buffer[i][6:])
	}
	return
}

func sendfile(data_conn *net.UDPConn, client *net.UDPAddr, file *os.File) {
	paquets, n := prepare_packets(file)
	timeouts := make([]time.Time, len(paquets))
	buf := make([]byte, 32)
	next_send_seq_num := 1
	last_received_ack := 0
	woffset := 1 //le plsu grand ACk reçu + 1

	dup_ack_num := 0

	send_paq := func(seq_num int) {
		if seq_num == len(paquets)-1 {
			data_conn.WriteTo(paquets[seq_num-1][0:n], client) //dernier paquet pas rempli
		} else {
			data_conn.WriteTo(paquets[seq_num-1], client)
		}
		timeouts[seq_num-1] = time.Now()
	}
	on_ack := func() {
		ack := parse_ack(string(buf[3:9]))
		//	fmt.Printf("ack %d %s\n",ack,string(buf))
		if ack == last_received_ack {
			dup_ack_num++
			if dup_ack_num > 3 {
				dup_ack_num = 0
				send_paq(ack + 1)
				fmt.Printf("dup_ack %d\n", ack)
			}
		}
		last_received_ack = ack
		if last_received_ack > woffset {
			woffset = last_received_ack + 1
		}

	}

	window_control := func() bool {
		if next_send_seq_num < woffset {
			next_send_seq_num = woffset
		}
		return next_send_seq_num < woffset+WSIZE
	}
	on_no_ack := func() {
		if window_control() {
			send_paq(next_send_seq_num)
			next_send_seq_num++
		} else {
			if time.Since(timeouts[woffset]) > TIMEOUT {
				send_paq(woffset)
			}
		}
	}

	for {

		if next_send_seq_num == len(paquets) {
			data_conn.WriteTo([]byte("FIN"), client)
			return
		}

		// imitation de select()
		e(data_conn.SetReadDeadline(time.Now().Add(time.Microsecond * 100)))
		_, re := data_conn.Read(buf)
		if re != nil {
			if strings.HasSuffix(re.Error(), "i/o timeout") { //pas reçu de ACK
				on_no_ack()
			} else {
				println("vraie erreur")
				panic(re)
			}
		} else { //reçu un ACK
			on_ack()
		}
		/*if nr > 0 {
			on_ack()
		}*/
		//fin imitation select()
	}
}
func rand_port() (newport int) {
	randint, re := rand.Int(rand.Reader, big.NewInt(8975))
	e(re)
	newport = int(randint.Int64() + 1024)
	return newport
}
func main() {

	waddr, _ := net.ResolveUDPAddr("udp", "0.0.0.0:5000")
	welcome_conn, wle := net.ListenUDP("udp", waddr)
	e(wle)

	newport := rand_port()

	daddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%04d", newport))
	data_conn, lde := net.ListenUDP("udp", daddr)
	e(lde)

	client, newport := welcome(welcome_conn, newport) //il faut ouvrir la socket avant car sinon on ne reçoit pas tout

	file := getfile(data_conn)
	fmt.Printf("%s %s\n", client.String(), file)

	started := time.Now()

	sendfile(data_conn, client, file)

	fmt.Printf("temps: %d\n", time.Since(started).Milliseconds()/1000)

}

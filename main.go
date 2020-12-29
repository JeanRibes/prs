package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

const (
	MTU          = 1400
	WSIZE        = 95
	TIMEOUT      = time.Millisecond * 50
	DUP_ACK_COUT = 2
	LOOP_WAIT    = time.Nanosecond * 10
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
		if string(buf)[0:3] == "SYN" {
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
	file, ferr := os.Open(filename)
	e(ferr)
	return file

}
func parse_ack(s string) (ack int) {
	fmt.Sscanf(s, "%06d", &ack)
	return ack
}
func fileSize(file *os.File) int64 {
	fi, _ := file.Stat()
	return fi.Size()
}

/*
prépare tous les paquets avec le numéro de séquence devant
*/
func prepare_packets(file *os.File) (buffer [][]byte, n int) {

	num_packets := (fileSize(file) / MTU) + 1
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
func progression(woffset *int, max_seq_num int) {
	m := float64(max_seq_num)
	for *woffset < max_seq_num {
		time.Sleep(time.Millisecond * 100)
		fmt.Printf("\r [%2.0f%%] %d", 100*float64(*woffset)/m, *woffset)
	}
}
func sendfile(data_conn *net.UDPConn, client *net.UDPAddr, file *os.File) {
	paquets, n := prepare_packets(file)
	timeouts := make([]time.Time, len(paquets)+2)
	buf := make([]byte, 32)
	max_seq_num := len(paquets)
	next_send_seq_num := 1
	last_received_ack := 0
	woffset := 1 //le plsu grand ACk reçu + 1

	dup_ack_num := 0

	//fmt.Printf("max se qunm %d\n",max_seq_num)

	send_paq := func(seq_num int) {
		if seq_num <= max_seq_num {
			if seq_num == max_seq_num {
				data_conn.WriteTo(paquets[seq_num-1][0:n], client) //dernier paquet pas rempli
			} else {
				data_conn.WriteTo(paquets[seq_num-1], client)
			}
			timeouts[seq_num-1] = time.Now()
		}
	}

	window_control := func() bool {
		if next_send_seq_num == max_seq_num {
			return false
		}
		if next_send_seq_num < woffset {
			next_send_seq_num = woffset
		}
		return next_send_seq_num < woffset+WSIZE
	}

	go func() {
		for woffset < max_seq_num {
			time.Sleep(LOOP_WAIT)
			if window_control() {
				send_paq(next_send_seq_num)
				next_send_seq_num++
			} else {
				if time.Since(timeouts[woffset]) > TIMEOUT { //-1 +1
					send_paq(woffset)
					//fmt.Printf("timeout %d \n", woffset)
				}
			}
		}
	}()
	go progression(&woffset, max_seq_num)
	for woffset < max_seq_num {
		_, re := data_conn.Read(buf)
		e(re)
		ack := parse_ack(string(buf[3:9]))
		//	fmt.Printf("ack %d %s\n",ack,string(buf))
		if ack == last_received_ack {
			dup_ack_num++
			if dup_ack_num > DUP_ACK_COUT {
				send_paq(ack + 1)
				//fmt.Printf("dup_ack %d\n", ack)
			}
		}
		if ack >= last_received_ack {
			last_received_ack = ack
		}
		if last_received_ack > woffset {
			woffset = last_received_ack + 1
		}
	}
	data_conn.WriteTo([]byte("FIN"), client)

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

	var newport int
	var data_conn *net.UDPConn
	lde := errors.New("nope")
	for lde != nil {
		newport = rand_port()
		daddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%04d", newport))
		data_conn, lde = net.ListenUDP("udp", daddr)
	}

	client, newport := welcome(welcome_conn, newport) //il faut ouvrir la socket avant car sinon on ne reçoit pas tout

	file := getfile(data_conn)

	started := time.Now()

	sendfile(data_conn, client, file)

	duree := time.Since(started)
	debit := float64(fileSize(file)) / duree.Seconds()
	fmt.Printf("temps: %f s, %f Mo/s\n",
		float32(duree.Milliseconds())/1000.0,
		debit/(1000*1000))

	time.Sleep(time.Millisecond * 1)
	data_conn.WriteTo([]byte("FIN"), client)

}

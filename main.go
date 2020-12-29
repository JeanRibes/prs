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
	MTU          = 1400
	WSIZE        = 60
	TIMEOUT      = time.Millisecond * 900
	DUP_ACK_COUT = 3
	LOOP_WAIT    = time.Millisecond * 1
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
func welcome(conn *net.UDPConn, newport int) *net.UDPAddr {
	buf := make([]byte, 100)
	for {
		clearbuf(buf)
		_, client0, _ := conn.ReadFromUDP(buf)
		if string(buf)[0:3] == "SYN" {
			clearbuf(buf)
			conn.WriteTo([]byte(fmt.Sprintf("SYN-ACK%04d", newport)), client0)
			_, client1, re := conn.ReadFromUDP(buf)
			e(re)
			if client0.String() == client1.String() {
				if string(buf)[0:3] == "ACK" {
					return client1
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
			if window_control() { //pour le début
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
		if window_control() { //pour le régime
			send_paq(next_send_seq_num)
			next_send_seq_num++
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
func save_stats(debit float64, size int64, duree int64) {
	statsFile, oe := os.OpenFile("stats.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
	e(oe)
	_, wse := statsFile.WriteString(fmt.Sprintf("%f Mo/s, %d o, %d s, %d wsize, %d timeout ns, %d dup_ack, %d loopwa_wait ns\n",
		debit, size, duree, WSIZE, TIMEOUT.Nanoseconds(), DUP_ACK_COUT, LOOP_WAIT.Nanoseconds()))
	e(wse)
	statsFile.Close()
}
func end_stats() {
	statsFile, _ := os.OpenFile("stats.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
	statsFile.WriteString("---------------------------------------------------------------------------------------")
	statsFile.Close()
}
func main() {

	waddr, _ := net.ResolveUDPAddr("udp", "0.0.0.0:5000")
	welcomeConn, wle := net.ListenUDP("udp", waddr)
	e(wle)
	end_stats()
	for {
		newPort := rand_port()
		daddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%04d", newPort))
		dataConn, _ := net.ListenUDP("udp", daddr)

		client := welcome(welcomeConn, newPort) //il faut ouvrir la socket avant car sinon on ne reçoit pas tout

		file := getfile(dataConn)

		started := time.Now()

		sendfile(dataConn, client, file)

		duree := time.Since(started)
		time.Sleep(time.Millisecond * 200)
		debit := float64(fileSize(file)) / duree.Seconds()
		fmt.Printf("\rtemps: %f s, %f Mo/s\n",
			float32(duree.Milliseconds())/1000.0,
			debit/(1000*1000))

		dataConn.WriteTo([]byte("FIN"), client)
		e(dataConn.Close())
		dataConn = nil

		save_stats(debit/(1000*1000), fileSize(file), duree.Milliseconds())
		file.Close()
	}

}

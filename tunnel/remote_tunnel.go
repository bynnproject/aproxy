package tunnel

import (
	"fmt"
	"github.com/treant5612/aproxy/encryption"
	"io"
	"log"
	"net"
	"strconv"
)

var headerSign = []byte("proxy")

type RemoteTunnelServer struct {
	*commonServer
}

func NewRemoteTunnelServer(key string) *RemoteTunnelServer {
	server := new(RemoteTunnelServer)
	server.commonServer = new(commonServer)
	server.Key = key
	return server
}
func (s *RemoteTunnelServer) ListenAndServe(network string, address string) (err error) {
	listener, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	log.Printf("listening on %s\n", address)
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

type RemoteTransporterClient struct {
	*commonClient
	serverAddr string
	serverPort string
}

func NewRemoteTunnel(key, remoteAddr, remotePort, dstAddr, dstPort string) (Tunnel, error) {
	return &RemoteTransporterClient{
		&commonClient{key, dstPort, dstAddr},
		remoteAddr,
		remotePort,
	}, nil
}
func (rc *RemoteTransporterClient) Transport(src io.ReadWriter) error {
	conn, err := net.Dial("tcp", net.JoinHostPort(rc.serverAddr, rc.serverPort))
	if err != nil {
		return err
	}
	defer conn.Close()
	return rc.doTransport(conn, src)

	erw, err := NewEncodeReadWriter(conn, rc.Key)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(rc.dstPort)
	if err != nil {
		return fmt.Errorf("wrong port ,%v", err)
	}
	portBytes := [2]byte{byte(port >> 8), byte(port & 0xff)}
	header := make([]byte, 8+len(rc.dstAddr))
	copy(header, headerSign)
	header[5] = byte(len(header) - 6)
	copy(header[6:], portBytes[:])
	copy(header[8:], rc.dstAddr)
	erw.Write(header)
	return doProxy(src, erw)
}
func (rc *RemoteTransporterClient) BindAddress() net.IP {
	return net.IPv4(0, 0, 0, 0)
}
func (rc *RemoteTransporterClient) BindPort() uint16 {
	return 0
}

type EncodeReadWriter struct {
	buf      []byte
	rw       io.ReadWriter
	encipher encryption.Encipher
}

func NewEncodeReadWriter(rw io.ReadWriter, key string) (erw *EncodeReadWriter, err error) {
	var encipher encryption.Encipher
	if key != "" {
		encipher, err = encryption.NewAesEncipher(key)
		if err != nil {
			return nil, err
		}
	}
	erw = &EncodeReadWriter{
		rw:       rw,
		encipher: encipher,
	}
	return erw, nil
}
func (e *EncodeReadWriter) Read(p []byte) (n int, err error) {
	defer func() {
		e := recover()
		if e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()
	//缓冲区中没有数据
	if e.buf == nil || len(e.buf) == 0 {
		lenInfo := [4]byte{}
		n, err := io.ReadFull(e.rw, lenInfo[:])
		if err != nil {
			return n, err
		}
		//用首部长度分割不同的加密数据包
		length := bytesToInt(lenInfo)
		//TODO 可重复使用缓冲区
		cipherBuf := make([]byte, length)
		n, err = io.ReadFull(e.rw, cipherBuf)
		if err != nil {
			return n, err
		}
		e.buf = e.encipher.Decrypt(cipherBuf)
	}
	n = copy(p, e.buf)
	e.buf = e.buf[n:]
	return n, nil

}
func (e *EncodeReadWriter) Write(p []byte) (n int, err error) {
	defer func() {
		e := recover()
		if e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()
	writing := p
	if e.encipher != nil {
		writing = e.encipher.Encrypt(p)
	}
	//以长度来设定数据包边界
	length := len(writing)
	wrappedWriting := make([]byte, len(writing)+4)
	intToBytes(length, wrappedWriting)
	//
	copy(wrappedWriting[4:], writing)
	_, err = e.rw.Write(wrappedWriting)
	return len(p), err
}

/*
	将int类型转为4字节的数组
*/
func intToBytes(n int, b []byte) {
	if len(b) < 4 {
		panic("length of slice < 4")
	}
	b[0], b[1], b[2], b[3] = byte(n>>24&0xff), byte(n>>16&0xff), byte(n>>8&0xff), byte(n&0xff)
}
func bytesToInt(b [4]byte) int {
	return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
}

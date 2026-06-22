package rcon

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	TypeResponseValue = 0
	TypeExecCommand   = 2
	TypeAuthResponse  = 2
	TypeAuth          = 3
)

type Client struct {
	conn net.Conn
	addr string
}

type Packet struct {
	Size int32
	ID   int32
	Type int32
	Body string
}

// Dial conecta al servidor RCON y se autentica.
func Dial(host string, port int, password string) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s: %w", addr, err)
	}

	client := &Client{conn: conn, addr: addr}

	if err := client.authenticate(password); err != nil {
		conn.Close()
		return nil, err
	}

	return client, nil
}

func (c *Client) authenticate(password string) error {
	// ID aleatorio o secuencia (usamos 1 para autenticación)
	req := &Packet{
		ID:   1,
		Type: TypeAuth,
		Body: password,
	}

	if err := c.writePacket(req); err != nil {
		return fmt.Errorf("error sending auth: %w", err)
	}

	// El primer paquete recibido puede ser SERVERDATA_RESPONSE_VALUE vacío en servidores Source
	// pero lo importante es el SERVERDATA_AUTH_RESPONSE.
	// Minecraft usualmente solo envía TypeAuthResponse directamente o seguido de un ResponseValue vacío.
	
	// Leer hasta encontrar el AuthResponse (o fallar tras 2 intentos)
	for i := 0; i < 2; i++ {
		resp, err := c.readPacket()
		if err != nil {
			return fmt.Errorf("error reading auth response: %w", err)
		}

		if resp.Type == TypeAuthResponse {
			if resp.ID == -1 {
				return fmt.Errorf("RCON authentication failed (incorrect password)")
			}
			return nil
		}
	}

	return fmt.Errorf("no authentication response received")
}

// Execute ejecuta un comando y devuelve el output del servidor.
func (c *Client) Execute(command string) (string, error) {
	req := &Packet{
		ID:   2,
		Type: TypeExecCommand,
		Body: command,
	}

	if err := c.writePacket(req); err != nil {
		return "", fmt.Errorf("error sending command: %w", err)
	}

	var output strings.Builder

	// Minecraft RCON puede fragmentar respuestas. Un truco estándar es enviar un
	// comando vacío o desconocido inmediatamente después para marcar el fin del stream
	// interceptando su respuesta, pero para la v0.1 leeremos un solo paquete
	// que suele contener toda la respuesta en la mayoría de implementaciones.
	
	// Establecer timeout corto de lectura para no quedarnos colgados
	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer c.conn.SetReadDeadline(time.Time{})

	for {
		resp, err := c.readPacket()
		if err != nil {
			// Si hay timeout y ya tenemos texto, devolvemos lo que tenemos.
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() && output.Len() > 0 {
				break
			}
			return "", fmt.Errorf("error reading response: %w", err)
		}

		if resp.ID == 2 && resp.Type == TypeResponseValue {
			output.WriteString(resp.Body)
			// Si es muy pequeño, probablemente terminó.
			if len(resp.Body) < 4096 {
				break
			}
		}
	}

	return output.String(), nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) writePacket(p *Packet) error {
	bodyBytes := []byte(p.Body)
	// Tamaño: ID(4) + Type(4) + Body(Len) + 2 null bytes finales
	p.Size = int32(8 + len(bodyBytes) + 2)

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, p.Size)
	binary.Write(buf, binary.LittleEndian, p.ID)
	binary.Write(buf, binary.LittleEndian, p.Type)
	buf.Write(bodyBytes)
	buf.Write([]byte{0, 0})

	_, err := c.conn.Write(buf.Bytes())
	return err
}

func (c *Client) readPacket() (*Packet, error) {
	var size int32
	if err := binary.Read(c.conn, binary.LittleEndian, &size); err != nil {
		return nil, err
	}

	if size < 10 || size > 4096*4 {
		return nil, fmt.Errorf("invalid packet size: %d", size)
	}

	buf := make([]byte, size)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return nil, err
	}

	p := &Packet{Size: size}
	
	reader := bytes.NewReader(buf)
	binary.Read(reader, binary.LittleEndian, &p.ID)
	binary.Read(reader, binary.LittleEndian, &p.Type)

	// Body va hasta el penúltimo byte (el último y penúltimo son nulos)
	bodyBytes := buf[8 : len(buf)-2]
	p.Body = string(bodyBytes)

	return p, nil
}

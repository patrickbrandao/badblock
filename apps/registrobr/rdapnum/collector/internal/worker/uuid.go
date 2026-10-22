package worker

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"time"
)

// NewUUID gera o uuid da instância na subida: um UUIDv7 (RFC 9562), como o
// uuidv7() do Postgres, para ele já sair na linha "iniciando" do log, antes
// da conexão com o banco.
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[6:])
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(time.Now().UnixMilli()))
	copy(b[:6], ms[2:])
	b[6] = b[6]&0x0f | 0x70 // versão 7
	b[8] = b[8]&0x3f | 0x80 // variante RFC 9562
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

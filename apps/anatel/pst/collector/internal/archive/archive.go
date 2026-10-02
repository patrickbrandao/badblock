// Package archive extrai o CSV do ZIP da Anatel.
//
// O ZIP é lido da memória (archive/zip) e tem de ter exatamente uma entrada
// terminada em .csv (sem distinguir caixa); as demais entradas são ignoradas
// e devolvidas em Ignored, para o aviso no log. O CSV descomprimido tem um
// limite (MaxCSVBytes), contra um ZIP corrompido ou uma "bomba" de
// compressão.
//
// A data de modificação da entrada vem do campo MS-DOS do ZIP (data e hora
// sem fuso), lida como hora de Brasília (UTC−3 fixo). O campo Modified do
// archive/zip não serve como está: sem timestamp estendido ele traz o relógio
// MS-DOS marcado como UTC (3 h adiantado), e com timestamp estendido (o ZIP
// da Anatel traz o NTFS, 0x000a) traz o instante do NTFS, que difere do
// MS-DOS em alguns segundos. Usar sempre o MS-DOS deixa a regra igual com ou
// sem timestamp estendido.
package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// MaxCSVBytes é o tamanho máximo do CSV descomprimido (512 MiB; o de
// 2026-09-30 tinha ~79 MB).
const MaxCSVBytes = 512 << 20

// Brasilia é o fuso da data das entradas do ZIP: UTC−3 fixo (sem horário de
// verão desde 2019).
var Brasilia = time.FixedZone("UTC-3", -3*60*60)

// CSV é a entrada CSV extraída do ZIP.
type CSV struct {
	Name       string
	Data       []byte
	SHA256     string // hex minúsculo
	ModifiedAt time.Time
	Ignored    []string // outras entradas do ZIP, ignoradas
}

// Extract lê o ZIP e devolve a única entrada .csv descomprimida.
func Extract(zipData []byte, maxBytes int64) (*CSV, error) {
	if maxBytes <= 0 {
		maxBytes = MaxCSVBytes
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("ZIP inválido: %w", err)
	}
	var csvs []*zip.File
	var ignored []string
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".csv") && !f.FileInfo().IsDir() {
			csvs = append(csvs, f)
		} else {
			ignored = append(ignored, f.Name)
		}
	}
	switch len(csvs) {
	case 0:
		return nil, fmt.Errorf("ZIP sem CSV")
	case 1:
	default:
		names := make([]string, len(csvs))
		for i, f := range csvs {
			names[i] = f.Name
		}
		return nil, fmt.Errorf("ZIP com %d arquivos CSV: %s", len(csvs), strings.Join(names, ", "))
	}
	f := csvs[0]
	if f.UncompressedSize64 > uint64(maxBytes) {
		return nil, fmt.Errorf("CSV %s maior que %d bytes descomprimido", f.Name, maxBytes)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("lendo %s do ZIP: %w", f.Name, err)
	}
	defer rc.Close()
	// O tamanho declarado pode mentir: o limite vale também na leitura. O
	// archive/zip confere o CRC-32 ao chegar ao fim da entrada.
	data, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("lendo %s do ZIP: %w", f.Name, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("CSV %s maior que %d bytes descomprimido", f.Name, maxBytes)
	}
	sum := sha256.Sum256(data)
	return &CSV{
		Name:       f.Name,
		Data:       data,
		SHA256:     hex.EncodeToString(sum[:]),
		ModifiedAt: dosTime(f),
		Ignored:    ignored,
	}, nil
}

// dosTime lê a data MS-DOS da entrada como hora de Brasília. Data MS-DOS
// inválida (mês ou dia zero) devolve o tempo zero: sem data, a checagem de
// "CSV mais antigo" não roda.
func dosTime(f *zip.File) time.Time {
	d, t := f.ModifiedDate, f.ModifiedTime //nolint:staticcheck // o campo MS-DOS é a regra da spec (ver o comentário do pacote)
	day, month, year := int(d&0x1f), int(d>>5&0x0f), int(d>>9)+1980
	if day == 0 || month == 0 || month > 12 {
		return time.Time{}
	}
	sec, minute, hour := int(t&0x1f)*2, int(t>>5&0x3f), int(t>>11)
	return time.Date(year, time.Month(month), day, hour, minute, sec, 0, Brasilia)
}

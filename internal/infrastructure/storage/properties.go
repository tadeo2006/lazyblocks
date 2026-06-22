package storage

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Properties represents a server.properties file
type Properties struct {
	path  string
	lines []string
	keys  map[string]int // Mapea la key al índice de la línea
}

// LoadProperties reads the server.properties file
func LoadProperties(path string) (*Properties, error) {
	p := &Properties{
		path: path,
		keys: make(map[string]int),
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, returning empty Properties
			return p, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		p.lines = append(p.lines, line)
		
		idx := len(p.lines) - 1
		lineTrimmed := strings.TrimSpace(line)
		
		// Ignorar comentarios y líneas vacías
		if lineTrimmed == "" || strings.HasPrefix(lineTrimmed, "#") {
			continue
		}

		parts := strings.SplitN(lineTrimmed, "=", 2)
		if len(parts) == 2 {
			p.keys[strings.TrimSpace(parts[0])] = idx
		}
	}

	return p, scanner.Err()
}

// Get devuelve el valor de una key
func (p *Properties) Get(key string, defaultVal string) string {
	idx, ok := p.keys[key]
	if !ok {
		return defaultVal
	}
	parts := strings.SplitN(p.lines[idx], "=", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return defaultVal
}

// Set cambia el valor de una key o la añade si no existe
func (p *Properties) Set(key, value string) {
	newLine := fmt.Sprintf("%s=%s", key, value)
	idx, ok := p.keys[key]
	if ok {
		p.lines[idx] = newLine
	} else {
		p.lines = append(p.lines, newLine)
		p.keys[key] = len(p.lines) - 1
	}
}

// Save writes changes to the file
func (p *Properties) Save() error {
	file, err := os.Create(p.path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range p.lines {
		fmt.Fprintln(writer, line)
	}
	return writer.Flush()
}

package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
	"tui.yamusic/internal/music"
)

const vaultIterations = 600000

var vaultMagic = []byte("YAMUSIC1")

type Session struct {
	Token     music.Token
	RefreshAt time.Time
}

func vaultCipher(password string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, vaultIterations, 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func sealSession(password string, session Session) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := vaultCipher(password, salt)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	plain, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	header := append(append(append([]byte{}, vaultMagic...), salt...), nonce...)
	return aead.Seal(header, nonce, plain, header), nil
}
func openSession(password string, data []byte) (Session, error) {
	var session Session
	if len(data) < 52 || string(data[:8]) != string(vaultMagic) {
		return session, errors.New("неподдерживаемый или повреждённый файл токена")
	}
	aead, err := vaultCipher(password, data[8:24])
	if err != nil {
		return session, err
	}
	headerEnd := 24 + aead.NonceSize()
	plain, err := aead.Open(nil, data[24:headerEnd], data[headerEnd:], data[:headerEnd])
	if err != nil {
		return session, errors.New("неверный пароль или повреждённый файл токена")
	}
	defer clear(plain)
	if err = json.Unmarshal(plain, &session); err != nil || session.Token.Access == "" {
		return Session{}, errors.New("повреждённые данные сессии")
	}
	return session, nil
}
func Save(path, password string, session Session) error {
	data, err := sealSession(password, session)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() { _ = os.Remove(temp) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}
func Load(path, password string) (Session, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Session{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return Session{}, errors.New("некорректный файл токена")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	return openSession(password, data)
}

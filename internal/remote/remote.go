// Package remote copies backup files to a remote host via SFTP.
// Optional: Verschlüsselung mit AES-256-CTR (Schlüssel aus remote_aes_password).
// Sync: Lokale Dateien hochladen wenn fehlend/älter; Remote-Dateien löschen die lokal nicht mehr existieren.
package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/janmz/mysqlbackup/internal/i18n"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/ssh"
)

const (
	saltLen        = 16
	nonceLen       = 16
	aesKeyLen      = 32
	pbkdf2Iter     = 100000
	encryptionOverhead = saltLen + nonceLen
)

var backupZipRe = regexp.MustCompile(`^mysql_backup_\d{8}_.*\.zip$`)

// localEntry holds name, modtime, size for a local backup zip.
type localEntry struct {
	Name    string
	ModTime time.Time
	Size    int64
	Path    string
}

// remoteEntry holds name, modtime, size for a remote file.
type remoteEntry struct {
	Name    string
	ModTime time.Time
	Size    int64
}

// Sync lists local backup zips and remote files; uploads local if missing or newer (optional AES-256);
// deletes remote files that are no longer present locally.
func Sync(cfg *config.Config, backupDir string, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
	Error(string, ...interface{})
}) error {
	if cfg.RemoteBackupDir == "" || cfg.RemoteSSHHost == "" {
		return nil
	}
	localList, err := listLocalBackups(backupDir)
	if err != nil {
		return fmt.Errorf(i18n.T("err.list_local"), err)
	}
	client, err := dial(cfg)
	if err != nil {
		return fmt.Errorf(i18n.T("err.ssh_dial"), err)
	}
	defer client.Close()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf(i18n.T("err.sftp"), err)
	}
	defer sftpClient.Close()
	remoteDir := filepath.ToSlash(cfg.RemoteBackupDir)
	if err := sftpClient.MkdirAll(remoteDir); err != nil && !os.IsExist(err) {
		log.Warn(i18n.Tf("log.warn.sftp_mkdir", remoteDir, err))
	}
	remoteList, err := listRemote(sftpClient, remoteDir)
	if err != nil {
		return fmt.Errorf(i18n.T("err.list_remote"), err)
	}
	remoteMap := make(map[string]remoteEntry)
	for _, e := range remoteList {
		remoteMap[e.Name] = e
	}
	aesPassword := strings.TrimSpace(cfg.RemoteAESPassword)
	encrypt := aesPassword != ""
	if encrypt {
		log.Info(i18n.T("log.msg.remote_aes_on"))
	} else {
		log.Info(i18n.T("log.msg.remote_aes_off"))
	}

	for _, loc := range localList {
		rem, exists := remoteMap[loc.Name]
		needUpload := !exists || loc.ModTime.After(rem.ModTime)
		if encrypt && exists {
			expectedSize := loc.Size + encryptionOverhead
			if rem.Size != expectedSize {
				needUpload = true
			}
		}
		if needUpload {
			remotePath := remoteDir + "/" + loc.Name
			if err := uploadFile(sftpClient, loc.Path, remotePath, encrypt, aesPassword); err != nil {
				return fmt.Errorf(i18n.Tf("err.upload", loc.Name), err)
			}
			log.Info(i18n.Tf("log.msg.uploaded", loc.Name))
		}
	}
	for _, rem := range remoteList {
		if _, inLocal := localListByName(localList, rem.Name); !inLocal {
			remotePath := remoteDir + "/" + rem.Name
			if err := sftpClient.Remove(remotePath); err != nil {
				log.Warn(i18n.Tf("log.warn.remote_remove", rem.Name, err))
				continue
			}
			log.Info(i18n.Tf("log.msg.removed_remote", rem.Name))
		}
	}
	return nil
}

func listLocalBackups(dir string) ([]localEntry, error) {
	dir = filepath.FromSlash(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []localEntry
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".zip" || !backupZipRe.MatchString(name) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		list = append(list, localEntry{
			Name:    name,
			ModTime: info.ModTime(),
			Size:    info.Size(),
			Path:    path,
		})
	}
	return list, nil
}

func localListByName(list []localEntry, name string) (localEntry, bool) {
	for _, e := range list {
		if e.Name == name {
			return e, true
		}
	}
	return localEntry{}, false
}

func listRemote(client *sftp.Client, remoteDir string) ([]remoteEntry, error) {
	entries, err := client.ReadDir(remoteDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []remoteEntry
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".zip" || !backupZipRe.MatchString(name) {
			continue
		}
		list = append(list, remoteEntry{
			Name:    name,
			ModTime: e.ModTime(),
			Size:    e.Size(),
		})
	}
	return list, nil
}

func uploadFile(client *sftp.Client, localPath, remotePath string, encrypt bool, aesPassword string) error {
	src, err := os.Open(filepath.FromSlash(localPath))
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := client.Create(remotePath)
	if err != nil {
		return err
	}
	defer dst.Close()
	if !encrypt {
		_, err = io.Copy(dst, src)
		return err
	}
	return streamEncryptUpload(src, dst, aesPassword)
}

// streamEncryptUpload streams plaintext from src, encrypts with AES-256-CTR, writes salt+nonce+ciphertext to dst.
func streamEncryptUpload(src io.Reader, dst io.Writer, password string) error {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf(i18n.T("err.rand_salt"), err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf(i18n.T("err.rand_nonce"), err)
	}
	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iter, aesKeyLen, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	stream := cipher.NewCTR(block, nonce)
	if _, err := dst.Write(salt); err != nil {
		return err
	}
	if _, err := dst.Write(nonce); err != nil {
		return err
	}
	w := &cipher.StreamWriter{S: stream, W: dst}
	_, err = io.Copy(w, src)
	return err
}

func dial(cfg *config.Config) (*ssh.Client, error) {
	var auth []ssh.AuthMethod
	if cfg.RemoteSSHKeyFile != "" {
		keyPath := filepath.FromSlash(cfg.RemoteSSHKeyFile)
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("err.read_key_file"), err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("err.parse_private_key"), err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.RemoteSSHPassword != "" {
		auth = append(auth, ssh.Password(cfg.RemoteSSHPassword))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf(i18n.T("err.no_ssh_auth"))
	}
	port := cfg.RemoteSSHPort
	if port <= 0 {
		port = 22
	}
	addr := fmt.Sprintf("%s:%d", cfg.RemoteSSHHost, port)
	sshConfig := &ssh.ClientConfig{
		User:            cfg.RemoteSSHUser,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	return ssh.Dial("tcp", addr, sshConfig)
}

// GetFile downloads one or more backup files from the remote server into destDir. The pattern
// may be a literal filename or contain wildcards (*, ?) matched on the remote side. No path
// components allowed in pattern (only base filename). If the remote file is encrypted, it is
// decrypted using remote_aes_password. Only .zip backup filenames (mysql_backup_YYYYMMDD_*.zip)
// are considered. Returns the list of local paths where files were saved.
func GetFile(cfg *config.Config, pattern, destDir string, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) ([]string, error) {
	if cfg.RemoteBackupDir == "" || cfg.RemoteSSHHost == "" {
		return nil, fmt.Errorf(i18n.T("err.remote_not_configured"))
	}
	if !validGetfilePattern(pattern) {
		return nil, fmt.Errorf(i18n.T("err.getfile_no_path"))
	}
	client, err := dial(cfg)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("err.ssh_dial"), err)
	}
	defer client.Close()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("err.sftp"), err)
	}
	defer sftpClient.Close()
	remoteDir := filepath.ToSlash(cfg.RemoteBackupDir)
	destDir = filepath.FromSlash(destDir)

	var toDownload []string
	if containsWildcard(pattern) {
		remoteList, err := listRemote(sftpClient, remoteDir)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("err.remote_list"), err)
		}
		for _, e := range remoteList {
			ok, err := filepath.Match(pattern, e.Name)
			if err != nil {
				return nil, fmt.Errorf(i18n.T("err.pattern"), err)
			}
			if ok {
				toDownload = append(toDownload, e.Name)
			}
		}
		if len(toDownload) == 0 {
			return nil, fmt.Errorf(i18n.Tf("err.no_remote_match", pattern))
		}
	} else {
		if filepath.Ext(pattern) != ".zip" || !backupZipRe.MatchString(pattern) {
			return nil, fmt.Errorf(i18n.T("err.only_backup_zip"))
		}
		toDownload = []string{pattern}
	}

	var saved []string
	for _, name := range toDownload {
		localPath := filepath.Join(destDir, name)
		if _, err := os.Stat(localPath); err == nil {
			localPath = filepath.Join(destDir, name+".lokal")
		}
		if err := getOneFile(sftpClient, remoteDir, name, localPath, cfg, log); err != nil {
			return saved, fmt.Errorf(i18n.Tf("err.file_failed", name), err)
		}
		saved = append(saved, localPath)
	}
	return saved, nil
}

// validGetfilePattern ensures pattern has no path components (no /, \, ..).
func validGetfilePattern(pattern string) bool {
	if pattern == "" || strings.Contains(pattern, "..") {
		return false
	}
	base := filepath.Base(pattern)
	return base == pattern && !strings.Contains(pattern, "/") && !strings.Contains(pattern, "\\")
}

func containsWildcard(s string) bool {
	return strings.Contains(s, "*") || strings.Contains(s, "?")
}

func getOneFile(client *sftp.Client, remoteDir, remoteName, localPath string, cfg *config.Config, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
}) error {
	remotePath := remoteDir + "/" + remoteName
	src, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf(i18n.T("err.remote_open"), err)
	}
	defer src.Close()
	header := make([]byte, saltLen+nonceLen)
	n, err := io.ReadFull(src, header)
	if err != nil && err != io.EOF {
		return fmt.Errorf(i18n.T("err.remote_read"), err)
	}
	aesPassword := strings.TrimSpace(cfg.RemoteAESPassword)
	decrypt := aesPassword != "" && n == saltLen+nonceLen && (header[0] != 'P' || header[1] != 'K')
	if decrypt {
		log.Info(i18n.Tf("log.msg.remote_decrypt", remoteName))
		key := pbkdf2.Key([]byte(aesPassword), header[0:saltLen], pbkdf2Iter, aesKeyLen, sha256.New)
		block, err := aes.NewCipher(key)
		if err != nil {
			return fmt.Errorf(i18n.T("err.cipher"), err)
		}
		stream := cipher.NewCTR(block, header[saltLen:saltLen+nonceLen])
		dst, err := os.Create(localPath)
		if err != nil {
			return fmt.Errorf(i18n.T("err.local_create"), err)
		}
		defer dst.Close()
		w := &cipher.StreamWriter{S: stream, W: dst}
		if _, err := io.Copy(w, src); err != nil {
			return fmt.Errorf(i18n.T("err.decrypt_write"), err)
		}
		return nil
	}
	dst, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf(i18n.T("err.local_create"), err)
	}
	defer dst.Close()
	if n > 0 {
		if _, err := dst.Write(header[:n]); err != nil {
			return err
		}
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf(i18n.T("err.copy"), err)
	}
	return nil
}

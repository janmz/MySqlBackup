// Package remote copies backup files to a remote host via SFTP.
package remote

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/janmz/mysqlbackup/internal/config"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Copy uploads the given local files to remote_backup_dir via SFTP.
func Copy(cfg *config.Config, localFiles []string, log interface {
	Info(string, ...interface{})
	Warn(string, ...interface{})
	Error(string, ...interface{})
}) error {
	if cfg.RemoteBackupDir == "" || cfg.RemoteSSHHost == "" {
		return nil
	}
	client, err := dial(cfg)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("sftp new client: %w", err)
	}
	defer sftpClient.Close()

	remoteDir := filepath.ToSlash(cfg.RemoteBackupDir)
	if err := sftpClient.MkdirAll(remoteDir); err != nil && !os.IsExist(err) {
		log.Warn("sftp mkdir %s: %v", remoteDir, err)
	}

	for _, localPath := range localFiles {
		name := filepath.Base(localPath)
		remotePath := remoteDir + "/" + name
		if err := uploadFile(sftpClient, localPath, remotePath); err != nil {
			return fmt.Errorf("upload %s: %w", name, err)
		}
		log.Info("uploaded %s to remote", name)
	}
	return nil
}

func dial(cfg *config.Config) (*ssh.Client, error) {
	var auth []ssh.AuthMethod
	if cfg.RemoteSSHKeyFile != "" {
		keyPath := filepath.FromSlash(cfg.RemoteSSHKeyFile)
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read key file: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.RemoteSSHPassword != "" {
		auth = append(auth, ssh.Password(cfg.RemoteSSHPassword))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("no SSH auth: set remote_ssh_key_file or remote_ssh_password")
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

func uploadFile(client *sftp.Client, localPath, remotePath string) error {
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

	_, err = io.Copy(dst, src)
	return err
}

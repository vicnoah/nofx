package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
)

// RSAKeyPair RSA密钥对
type RSAKeyPair struct {
	PrivateKey string `json:"private_key"` // PEM格式的私钥
	PublicKey  string `json:"public_key"`  // PEM格式的公钥
}

// GenerateRSAKeyPair 生成RSA密钥对（2048位）
func GenerateRSAKeyPair() (*RSAKeyPair, error) {
	// 生成2048位RSA密钥对
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("生成RSA密钥失败: %w", err)
	}

	// 编码私钥为PEM格式
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	// 编码公钥为PEM格式
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("编码公钥失败: %w", err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return &RSAKeyPair{
		PrivateKey: string(privateKeyPEM),
		PublicKey:  string(publicKeyPEM),
	}, nil
}

// ParseRSAPrivateKey 解析PEM格式的私钥
func ParseRSAPrivateKey(privateKeyPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("解析私钥PEM失败")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}

	return privateKey, nil
}

// ParseRSAPublicKey 解析PEM格式的公钥
func ParseRSAPublicKey(publicKeyPEM string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("解析公钥PEM失败")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析公钥失败: %w", err)
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("公钥类型不正确")
	}

	return rsaPubKey, nil
}

// RSAEncrypt 使用公钥加密数据
func RSAEncrypt(publicKeyPEM string, plaintext string) (string, error) {
	publicKey, err := ParseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return "", err
	}

	// 使用OAEP填充模式加密
	ciphertext, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		publicKey,
		[]byte(plaintext),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("RSA加密失败: %w", err)
	}

	// 返回Base64编码的密文
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// RSADecrypt 使用私钥解密数据
func RSADecrypt(privateKeyPEM string, ciphertextBase64 string) (string, error) {
	privateKey, err := ParseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}

	// Base64解码密文
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return "", fmt.Errorf("Base64解码失败: %w", err)
	}

	// 使用OAEP填充模式解密
	plaintext, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		privateKey,
		ciphertext,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("RSA解密失败: %w", err)
	}

	return string(plaintext), nil
}

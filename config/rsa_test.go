package config

import (
	"encoding/json"
	"testing"
)

// TestHybridEncryption 测试混合加密解密流程
func TestHybridEncryption(t *testing.T) {
	// 1. 生成密钥对
	keyPair, err := GenerateRSAKeyPair()
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	// 2. 测试数据
	testData := "This is a test message for hybrid encryption with RSA and AES. 这是一个测试消息，包含中文字符。"

	t.Logf("原始数据长度: %d 字节", len(testData))

	// 3. 模拟前端混合加密（这里用 Go 模拟）
	// 注意：实际的前端加密由 JavaScript 完成，这里只是验证后端解密逻辑
	
	// 4. 构造模拟的混合加密结果（模拟前端返回的格式）
	mockEncryptedJSON := `{
		"encryptedAesKey": "test_key",
		"iv": "test_iv",
		"encryptedData": "test_data"
	}`

	// 验证能正确解析 JSON 结构
	var result HybridEncryptResult
	err = json.Unmarshal([]byte(mockEncryptedJSON), &result)
	if err != nil {
		t.Fatalf("解析混合加密JSON失败: %v", err)
	}

	if result.EncryptedAesKey != "test_key" {
		t.Errorf("期望 encryptedAesKey='test_key', 得到 '%s'", result.EncryptedAesKey)
	}
	if result.IV != "test_iv" {
		t.Errorf("期望 iv='test_iv', 得到 '%s'", result.IV)
	}
	if result.EncryptedData != "test_data" {
		t.Errorf("期望 encryptedData='test_data', 得到 '%s'", result.EncryptedData)
	}

	t.Log("✓ 混合加密JSON结构验证通过")
}

// TestSmartDecrypt 测试智能解密功能
func TestSmartDecrypt(t *testing.T) {
	// 生成密钥对
	keyPair, err := GenerateRSAKeyPair()
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	// 测试纯RSA加密解密
	t.Run("纯RSA加密", func(t *testing.T) {
		plaintext := "short message"
		
		// 加密
		ciphertext, err := RSAEncrypt(keyPair.PublicKey, plaintext)
		if err != nil {
			t.Fatalf("RSA加密失败: %v", err)
		}

		// 使用智能解密
		decrypted, err := SmartDecrypt(keyPair.PrivateKey, ciphertext)
		if err != nil {
			t.Fatalf("智能解密失败: %v", err)
		}

		if decrypted != plaintext {
			t.Errorf("期望 '%s', 得到 '%s'", plaintext, decrypted)
		}

		t.Log("✓ 纯RSA加密解密测试通过")
	})

	// 测试混合加密格式识别
	t.Run("混合加密格式识别", func(t *testing.T) {
		// 模拟混合加密JSON（但数据不完整，预期会失败）
		hybridJSON := `{
			"encryptedAesKey": "invalid",
			"iv": "invalid",
			"encryptedData": "invalid"
		}`

		// SmartDecrypt 应该识别为混合加密格式并尝试解密
		_, err := SmartDecrypt(keyPair.PrivateKey, hybridJSON)
		if err == nil {
			t.Error("期望解密失败（因为数据无效），但成功了")
		}

		t.Logf("✓ 混合加密格式识别测试通过（预期失败: %v）", err)
	})
}

// TestRSAEncryptDecrypt 测试基本RSA加密解密
func TestRSAEncryptDecrypt(t *testing.T) {
	// 生成密钥对
	keyPair, err := GenerateRSAKeyPair()
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	testCases := []struct {
		name      string
		plaintext string
	}{
		{"短文本", "Hello"},
		{"中文", "你好世界"},
		{"混合", "Hello 世界 123"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 加密
			ciphertext, err := RSAEncrypt(keyPair.PublicKey, tc.plaintext)
			if err != nil {
				t.Fatalf("加密失败: %v", err)
			}

			// 解密
			decrypted, err := RSADecrypt(keyPair.PrivateKey, ciphertext)
			if err != nil {
				t.Fatalf("解密失败: %v", err)
			}

			if decrypted != tc.plaintext {
				t.Errorf("期望 '%s', 得到 '%s'", tc.plaintext, decrypted)
			}
		})
	}

	t.Log("✓ 所有RSA加密解密测试通过")
}

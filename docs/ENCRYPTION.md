# 加密方案说明

## 概述

本项目采用 **RSA + AES 混合加密方案**，用于保护敏感数据在前后端传输过程中的安全性。

**重要：**
- 前端必须使用混合加密，所有敏感数据均需加密
- 后端接口仅接受加密数据，拒绝明文请求
- 支持智能解密，自动识别加密格式

## 技术选型

### 前端
- **实现**：浏览器原生 Web Crypto API
- **数据加密**：AES-GCM-256
- **密钥加密**：RSA-OAEP with SHA-256
- **密钥长度**：RSA 2048位

### 后端
- **实现**：Go 标准库 crypto 包
- **数据加密**：AES-GCM
- **密钥加密**：RSA-OAEP with SHA-256

## 加密流程

### 前端加密流程

```typescript
// 1. 获取服务器公钥（首次获取后缓存）
const publicKey = await fetch('/api/rsa-public-key');

// 2. 生成随机 AES-256 密钥
const aesKey = crypto.subtle.generateKey({
  name: 'AES-GCM',
  length: 256
}, true, ['encrypt']);

// 3. 生成随机初始化向量 (IV)
const iv = crypto.getRandomValues(new Uint8Array(12));

// 4. 使用 AES-GCM 加密数据
const encryptedData = await crypto.subtle.encrypt(
  { name: 'AES-GCM', iv },
  aesKey,
  plainText
);

// 5. 使用 RSA 公钥加密 AES 密钥
const encryptedAesKey = await crypto.subtle.encrypt(
  { name: 'RSA-OAEP' },
  publicKey,
  aesKey
);

// 6. 返回 JSON 格式
{
  "encryptedAesKey": "base64...",  // RSA加密的AES密钥
  "iv": "base64...",                // 初始化向量
  "encryptedData": "base64..."      // AES加密的数据
}
```

### 后端解密流程

```go
// 1. 解析前端发送的 JSON
type HybridEncryptResult struct {
    EncryptedAesKey string `json:"encryptedAesKey"`
    IV              string `json:"iv"`
    EncryptedData   string `json:"encryptedData"`
}

// 2. 使用服务器私钥解密 AES 密钥
aesKey := rsa.DecryptOAEP(sha256, rand, privateKey, encryptedAesKey, nil)

// 3. 使用 AES 密钥和 IV 解密数据
plaintext := aesgcm.Open(nil, iv, encryptedData, nil)
```

## 使用示例

### 前端使用

```typescript
import RSAEncrypt, { encryptData, encryptJSON } from '@/lib/encryption';

// 方式1: 加密字符串
const encrypted = await RSAEncrypt.encrypt('sensitive data');

// 方式2: 加密密码
const encryptedPassword = await RSAEncrypt.encryptPassword('password123');

// 方式3: 加密 JSON 对象
const encryptedJson = await encryptJSON({
  username: 'user@example.com',
  password: 'password123'
});

// 方式4: 兼容旧接口
const encrypted = await encryptData('data');
```

### 后端使用

```go
import "github.com/yourusername/nofx/config"

// 方法1: 智能解密（推荐）- 自动识别加密格式
plaintext, err := config.SmartDecrypt(privateKey, ciphertext)

// 方法2: 通过 Database 对象
plaintext, err := database.DecryptRSAData(ciphertext)

// 方法3: 显式使用混合解密
plaintext, err := config.HybridDecrypt(privateKey, hybridJSON)
```

**注意：** 后端接口不再接受非加密数据，所有请求必须包含加密后的 `data` 字段。

## 安全特性

### 1. 密钥管理
- RSA 密钥对在服务器启动时生成并存储在数据库
- 私钥仅保存在服务器端，永不暴露
- 公钥通过 API 端点提供给前端

### 2. 数据安全
- 支持任意长度的数据加密（无 RSA 190字节限制）
- AES-GCM 提供认证加密（AEAD）
- 每次加密使用随机 AES 密钥和 IV

### 3. 向后兼容
- 智能解密函数自动识别加密格式
- 同时支持纯 RSA 和混合加密格式
- 前端旧接口继续可用

## API 端点

### 获取公钥
```
GET /api/rsa-public-key
```

**响应示例：**
```json
{
  "public_key": "-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"
}
```

### 需要加密的接口

以下接口**必须**使用加密数据：

1. **用户注册** - `POST /api/register`
   ```json
   {
     "email": "user@example.com",
     "password": "<加密后的密码>"
   }
   ```

2. **用户登录** - `POST /api/login`
   ```json
   {
     "email": "user@example.com",
     "password": "<加密后的密码>"
   }
   ```

3. **更新模型配置** - `PUT /api/models`
   ```json
   {
     "data": "<加密后的JSON数据>"
   }
   ```

4. **更新交易所配置** - `PUT /api/exchanges`
   ```json
   {
     "data": "<加密后的JSON数据>"
   }
   ```

## 性能考虑

- **AES 加密**：速度快，适合大数据量
- **RSA 加密**：仅用于加密 32 字节的 AES 密钥
- **公钥缓存**：前端缓存公钥，减少网络请求

## 注意事项

1. **前端必须使用加密**：所有敏感数据必须使用混合加密
2. **后端强制加密**：接口仅接受加密数据，拒绝明文请求
3. **HTTPS 传输**：生产环境必须启用 HTTPS
4. **密钥轮换**：建议定期轮换 RSA 密钥对
5. **错误处理**：加密失败应妥善处理，避免泄露敏感信息

## 测试

### 前端测试
```bash
cd web
npm test
```

### 后端测试
```bash
go test ./config/...
```

## 相关文件

### 前端
- `/web/src/lib/encryption.ts` - 加密工具类

### 后端
- `/config/rsa.go` - RSA 和混合加密实现
- `/config/database.go` - 数据库集成
- `/config/rsa_test.go` - 单元测试

## 更新日志

### 2025-11-04
- 实现 RSA + AES 混合加密方案
- 前端使用 Web Crypto API
- 后端智能解密支持
- 添加完整的单元测试
- **强制加密**：移除 `encrypted` 标志，所有接口强制要求加密数据

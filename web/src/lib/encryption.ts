/**
 * RSA + AES 混合加密工具类
 * 使用 Web Crypto API 实现安全的混合加密方案
 * - AES-GCM-256 用于数据加密（支持任意长度）
 * - RSA-OAEP 用于加密 AES 密钥
 * 用于敏感数据加密传输
 * 
 * @example
 * // 加密普通文本
 * const encrypted = await RSAEncrypt.encrypt('sensitive data');
 * 
 * @example
 * // 加密密码
 * const encryptedPassword = await RSAEncrypt.encryptPassword('myPassword123');
 * 
 * @example
 * // 加密JSON对象
 * const encryptedJson = await encryptJSON({ username: 'user', password: 'pass' });
 */

/**
 * 混合加密结果接口
 */
interface HybridEncryptResult {
  encryptedAesKey: string;  // RSA加密的AES密钥（Base64）
  iv: string;                // AES加密的初始化向量（Base64）
  encryptedData: string;     // AES加密的数据（Base64）
}

/**
 * RSA + AES 混合加密工具
 */
class RSAEncrypt {
  private static publicKey: CryptoKey | null = null;
  private static publicKeyPEM: string | null = null;

  /**
   * 获取公钥 PEM 字符串
   */
  private static async getPublicKeyPEM(): Promise<string> {
    if (!this.publicKeyPEM) {
      try {
        const response = await fetch('/api/rsa-public-key');
        if (!response.ok) {
          throw new Error('获取RSA公钥失败');
        }
        const data = await response.json();
        this.publicKeyPEM = data.public_key;
      } catch (error) {
        console.error('获取RSA公钥失败:', error);
        throw new Error('获取RSA公钥失败');
      }
    }
    return this.publicKeyPEM as string;
  }

  /**
   * 从 PEM 格式导入公钥
   */
  private static async importPublicKey(): Promise<CryptoKey> {
    if (!this.publicKey) {
      try {
        const pem = await this.getPublicKeyPEM();
        
        // 移除 PEM 头尾和换行符
        const pemContents = pem
          .replace(/-----BEGIN PUBLIC KEY-----/, '')
          .replace(/-----END PUBLIC KEY-----/, '')
          .replace(/\s/g, '');

        // Base64 解码
        const binaryDer = Uint8Array.from(atob(pemContents), c => c.charCodeAt(0));
        
        // 导入公钥
        this.publicKey = await window.crypto.subtle.importKey(
          'spki',
          binaryDer,
          {
            name: 'RSA-OAEP',
            hash: 'SHA-256'
          },
          true,
          ['encrypt']
        );
      } catch (err) {
        console.error('公钥导入失败:', err);
        throw new Error(`公钥导入失败: ${err}`);
      }
    }
    return this.publicKey;
  }

  /**
   * RSA + AES 混合加密
   * 适用于任意长度的数据加密
   * @param plainText 明文
   * @returns 加密后的密文（JSON字符串格式）
   */
  static async encrypt(plainText: string): Promise<string> {
    try {
      // 1. 生成随机的 AES 密钥
      const aesKey = await window.crypto.subtle.generateKey(
        {
          name: 'AES-GCM',
          length: 256,
        },
        true,
        ['encrypt']
      );

      // 2. 生成随机 IV
      const iv = window.crypto.getRandomValues(new Uint8Array(12));

      // 3. 使用 AES 加密数据
      const encoder = new TextEncoder();
      const dataBuffer = encoder.encode(plainText);
      
      const encryptedData = await window.crypto.subtle.encrypt(
        {
          name: 'AES-GCM',
          iv: iv,
        },
        aesKey,
        dataBuffer
      );

      // 4. 导出 AES 密钥（用于 RSA 加密）
      const exportedAesKey = await window.crypto.subtle.exportKey('raw', aesKey);
      
      // 5. 使用 RSA 加密 AES 密钥
      const publicKey = await this.importPublicKey();
      const encryptedAesKey = await window.crypto.subtle.encrypt(
        { name: 'RSA-OAEP' },
        publicKey,
        exportedAesKey
      );

      // 6. 组合所有数据
      const result: HybridEncryptResult = {
        encryptedAesKey: btoa(String.fromCharCode(...new Uint8Array(encryptedAesKey))),
        iv: btoa(String.fromCharCode(...iv)),
        encryptedData: btoa(String.fromCharCode(...new Uint8Array(encryptedData)))
      };

      return JSON.stringify(result);
    } catch (error) {
      console.error('混合加密失败:', error);
      throw error;
    }
  }

  /**
   * 加密密码
   * @param password 密码明文
   * @returns 加密后的密码
   */
  static async encryptPassword(password: string): Promise<string> {
    return this.encrypt(password);
  }

  /**
   * 清除缓存的公钥（用于测试或重新初始化）
   */
  static clearCache(): void {
    this.publicKey = null;
    this.publicKeyPEM = null;
  }
}

// 导出兼容旧接口的函数

/**
 * 获取RSA公钥（带缓存）
 * @deprecated 建议直接使用 RSAEncrypt.encrypt
 */
export async function getPublicKey(): Promise<string> {
  return RSAEncrypt['getPublicKeyPEM']();
}

/**
 * 使用混合加密方式加密数据
 */
export async function encryptData(plaintext: string): Promise<string> {
  return RSAEncrypt.encrypt(plaintext);
}

/**
 * 加密JSON对象（用于整体加密请求）
 */
export async function encryptJSON(data: any): Promise<string> {
  const jsonString = JSON.stringify(data);
  return RSAEncrypt.encrypt(jsonString);
}

/**
 * 清除缓存的公钥（例如在退出登录时）
 */
export function clearPublicKeyCache(): void {
  RSAEncrypt.clearCache();
}

// 默认导出加密工具类
export default RSAEncrypt;

// 导出类型定义
export type { HybridEncryptResult };

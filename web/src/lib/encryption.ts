import JSEncrypt from 'jsencrypt';

let cachedPublicKey: string | null = null;

/**
 * 获取RSA公钥（带缓存）
 */
export async function getPublicKey(): Promise<string> {
  if (cachedPublicKey) {
    return cachedPublicKey as string;
  }

  try {
    const response = await fetch('/api/rsa-public-key');
    if (!response.ok) {
      throw new Error('获取RSA公钥失败');
    }
    const data = await response.json();
    cachedPublicKey = data.public_key;
    return cachedPublicKey as string;
  } catch (error) {
    console.error('获取RSA公钥失败:', error);
    throw error;
  }
}

/**
 * 使用RSA公钥加密数据
 */
export async function encryptData(plaintext: string): Promise<string> {
  const publicKey = await getPublicKey();
  const encrypt = new JSEncrypt();
  encrypt.setPublicKey(publicKey);
  
  const encrypted = encrypt.encrypt(plaintext);
  if (!encrypted || typeof encrypted !== 'string') {
    throw new Error('RSA加密失败');
  }
  
  return encrypted;
}

/**
 * 加密JSON对象（用于整体加密请求）
 */
export async function encryptJSON(data: any): Promise<string> {
  const jsonString = JSON.stringify(data);
  return encryptData(jsonString);
}

/**
 * 清除缓存的公钥（例如在退出登录时）
 */
export function clearPublicKeyCache() {
  cachedPublicKey = null;
}

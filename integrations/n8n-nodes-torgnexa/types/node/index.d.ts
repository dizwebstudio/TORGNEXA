declare module 'node:crypto' {
  export function createHash(algorithm: string): { update(data: string | Uint8Array): any; digest(encoding: 'hex'): string };
  export function createHmac(algorithm: string, key: string | Uint8Array): { update(data: string | Uint8Array): any; digest(): Uint8Array };
  export function randomBytes(size: number): { toString(encoding: 'hex'): string };
  export function timingSafeEqual(a: Uint8Array, b: Uint8Array): boolean;
}
declare class Buffer extends Uint8Array {
  static from(value: string, encoding?: string): Buffer;
  static isBuffer(value: any): boolean;
  toString(encoding?: string): string;
}

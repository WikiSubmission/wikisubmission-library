import crypto from "crypto";

interface CachedFile {
    publicUrl: string;
    metadata?: {
        contentType?: string;
        size?: number;
        etag?: string;
    };
    failedAttempts?: number;
    lastFailure?: number;
}

interface FileMetadata {
    contentType?: string;
    size?: number;
}

const fileCache = new Map<string, CachedFile>();
const BROWSER_CACHE_DURATION = 7 * 24 * 60 * 60; // 7 days
const FAILURE_THRESHOLD = 1; // Blacklist after 1 failure
const FAILURE_WINDOW = 5 * 60 * 1000; // 5 minutes

export function getCachedFile(path: string): CachedFile | undefined {
    const cached = fileCache.get(path);
    if (!cached) return undefined;

    // Skip if path is blacklisted
    if (isBlacklisted(cached)) {
        return undefined;
    }

    return cached;
}

export function setCachedFile(path: string, publicUrl: string, metadata?: FileMetadata): void {
    fileCache.set(path, {
        publicUrl,
        metadata: metadata ? { ...metadata, etag: generateETag(path) } : undefined,
        failedAttempts: 0
    });
}

export function recordFailure(path: string): void {
    const cached = fileCache.get(path) || { publicUrl: '', failedAttempts: 0 };
    cached.failedAttempts = (cached.failedAttempts || 0) + 1;
    cached.lastFailure = Date.now();
    fileCache.set(path, cached);
}

export function getBrowserCacheDuration(): number {
    return BROWSER_CACHE_DURATION;
}

function generateETag(path: string): string {
    return crypto.createHash('md5').update(path).digest('hex');
}

function isBlacklisted(cached: CachedFile): boolean {
    if (!cached.failedAttempts || cached.failedAttempts < FAILURE_THRESHOLD) {
        return false;
    }

    const timeSinceLastFailure = Date.now() - (cached.lastFailure || 0);
    if (timeSinceLastFailure < FAILURE_WINDOW) {
        return true;
    }

    // Reset failure count if window has passed
    cached.failedAttempts = 0;
    return false;
} 
import path from "path";
import { getSupabaseClient } from "./get-supabase-client";
import { WFileResult } from "../types/w-file-result";

export async function getMatchingFiles(components: string[]): Promise<WFileResult[]> {
    const db = getSupabaseClient();

    const fileHint = components.at(-1)?.toLowerCase() || "";

    // [Step 1: Attempt direct match from first component as bucket name]
    for (const [index, component] of components.entries()) {
        const bucketCheck = await db.storage.from(component).list();
        if (!bucketCheck.data || bucketCheck.data.length === 0) continue;

        const relativePath = components.slice(index + 1).join("/");
        const parentFolder = relativePath.split("/").slice(0, -1).join("/");

        const files = await db.storage.from(component).list(parentFolder, { limit: 1000 });

        const matches = extractMatches(files.data, component, fileHint);
        if (matches.length > 0) return matches;
    }

    // [Step 2: Fallback search across all buckets]
    const allBuckets = await db.storage.listBuckets();
    const results: WFileResult[] = [];

    for (const bucket of allBuckets.data ?? []) {
        const contents = await db.storage.from(bucket.name).list("", {
            limit: 1000,
            sortBy: { column: "name", order: "asc" }
        });

        const matches = extractMatches(contents.data, bucket.name, fileHint);
        results.push(...matches);
    }

    return results.sort((a, b) => b.score - a.score);
}

function extractMatches(
    files: { name: string; metadata?: Record<string, any> }[] | null,
    bucket: string,
    hint: string
): WFileResult[] {
    if (!files) return [];

    return files
        .filter(f => !f.name.endsWith("/"))
        .map(f => {
            const name = f.name;
            return {
                folder: bucket,
                file: name,
                path: `${bucket}/${name}`,
                extension: f.metadata?.mimetype || name.split(".").pop()?.toLowerCase() || "unknown",
                score: scoreMatch(name, hint),
                url: `https://library.wikisubmission.org/file/${name}`,
                urls: [
                    {
                        provider: "WikiSubmission",
                        url: `https://library.wikisubmission.org/file/${name}`
                    },
                    {
                        provider: "Supabase",
                        url: `https://uunhgbgnjwcdnhmgadra.supabase.co/storage/v1/object/public/${bucket}/${name}`
                    }
                ]
            };
        })
        .filter(f => f.score > 0)
        .sort((a, b) => b.score - a.score);
}

function scoreMatch(fileName: string, target: string): number {
    const normalize = (str: string) =>
        str.toLowerCase()
            .replace(/[_\-]+/g, " ")
            .replace(/[^a-z0-9 ]+/g, "")
            .replace(/\s+/g, " ")
            .trim();

    const baseName = normalize(fileName.split(".")?.[0]);
    const targetBase = normalize(target.split(".")?.[0]);

    if (!baseName || !targetBase) return 0;

    const baseWords = new Set(baseName.split(" "));
    const targetWords = new Set(targetBase.split(" "));

    let shared = 0;
    for (const word of targetWords) {
        if (baseWords.has(word)) shared++;
    }

    const coverage = shared / targetWords.size;
    let score = 0;

    if (coverage === 1) score += 100;
    else if (coverage >= 0.75) score += 85;
    else if (coverage >= 0.5) score += 65;
    else if (coverage >= 0.25) score += 40;
    else score += shared * 10;

    score -= Math.abs(baseName.length - targetBase.length) * 0.5;

    return score;
}
import { WRoute } from "../types/w-route";
import { getMatchingFiles } from "../utils/get-matching-file";
import { getSupabaseClient } from "../utils/get-supabase-client";
import { getCachedFile, setCachedFile, getBrowserCacheDuration, recordFailure } from "../utils/file-cache";

/**
 * Returns the actual file requested, on a best-effort basis as URL syntax may slightly vary.
 * Proxies through Supabase's CDN.
 */
export default function route(): WRoute {
    return {
        url: "/file/*",
        method: "GET",
        handler: async (request, reply) => {
            try {
                const components = request.url.replace(/^\/file\//, "").split("/").filter(Boolean);
                const result = await getMatchingFiles(components);

                const best = result?.[0];
                if (!best) {
                    return reply.code(404).send({
                        error: `No file matched with '${components.join("/")}' – try a different path?`
                    });
                }

                const cacheKey = best.path;
                const cached = getCachedFile(cacheKey);

                let publicUrl: string;

                if (cached?.publicUrl) {
                    publicUrl = cached.publicUrl;
                } else {
                    try {
                        const db = getSupabaseClient();
                        const { data: pub } = db.storage.from(best.folder).getPublicUrl(best.file);
                        publicUrl = pub.publicUrl;
                        setCachedFile(cacheKey, publicUrl, {});
                    } catch (error) {
                        recordFailure(cacheKey);
                        return reply.code(500).send({
                            error: `Failed to access file at '${best.path}'`
                        });
                    }
                }

                reply.header("content-disposition", `inline; filename="${best.file.split("/").pop() || best.file}"`);
                reply.header("cache-control", `public, max-age=${getBrowserCacheDuration()}, immutable`);

                return reply.from(publicUrl);
            } catch (error) {
                return reply.code(500).send({
                    error: "Internal Server Error",
                    message: error instanceof Error ? error.message : "Unknown error"
                });
            }
        }
    };
}
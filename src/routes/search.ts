import { WRoute } from "../types/w-route";
import { getMatchingFiles } from "../utils/get-matching-file";

/**
 * Returns closest files that match the search query.
 */
export default function route(): WRoute {
    return {
        url: "/search/*",
        method: "GET",
        handler: async (request, reply) => {
            const components = request.url.replace(/^\/search\//, "").split("/").filter(Boolean);
            const result = await getMatchingFiles(components);

            if (result.length === 0) {
                return reply.code(404).send({
                    message: "No files found",
                    files: []
                });
            }

            return reply.code(200).send({
                message: `Found ${result.length} matching files`,
                files: result
            });
        }
    };
}
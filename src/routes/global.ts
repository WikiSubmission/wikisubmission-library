import { FastifyReply, FastifyRequest } from "fastify";
import { WRoute } from "../types/w-route";
import { createReadStream } from "fs";
import path from "path";
import fs from "fs/promises";

export default function route(): WRoute {
    return {
        url: "*",
        method: "GET",
        handler: async (request: FastifyRequest, reply: FastifyReply) => {
            const url = request.url;

            // [Health check]
            if (url === "/health") {
                return reply.send({
                    status: "ok",
                    timestamp: new Date().toISOString()
                });
            }

            // [Public files]
            try {
                const publicPath = path.join(process.cwd(), "src/public", url);
                const stats = await fs.stat(publicPath);

                if (stats.isFile()) {
                    const ext = path.extname(url).toLowerCase();
                    const contentType = {
                        '.ico': 'image/x-icon',
                        '.png': 'image/png',
                        '.jpg': 'image/jpeg',
                        '.jpeg': 'image/jpeg',
                        '.gif': 'image/gif',
                        '.svg': 'image/svg+xml',
                        '.css': 'text/css',
                        '.js': 'application/javascript',
                        '.json': 'application/json',
                        '.html': 'text/html',
                    }[ext] || 'application/octet-stream';

                    reply.header('Content-Type', contentType);
                    return reply.send(createReadStream(publicPath));
                }
            } catch (error) {
                // File not found or other error
                return reply.code(404).send({
                    error: "Not Found",
                    message: "The requested resource was not found"
                });
            }

            return reply.code(404).send({
                error: "Not Found",
                message: "The requested resource was not found"
            });
        },
    };
} 
import Fastify, { FastifyInstance } from "fastify";
import fastifyCors from "@fastify/cors";
import fastifyHelmet from "@fastify/helmet";
import fastifyReplyFrom from "@fastify/reply-from";
import { getFileExports } from "../utils/get-file-exports";
import { WRoute } from "../types/w-route";

export class Server {
    static instance = new Server();

    server: FastifyInstance;

    port = 8080;

    constructor() {
        this.server = Fastify({
            logger: {
                enabled: true,
                transport: {
                    targets: [
                        // [Pino Pretty - Pretty logs]
                        {
                            target: "pino-pretty",
                            options: {
                                translateTime: true,
                                ignorePaths: ["req.headers", "req.body"],
                                colorize: true,
                                singleLine: true,
                                messageFormat:
                                    "{msg}{if req} [{req.id}] {req.method} \"{req.url}\"{end}{if res} [{res.id}] {res.statusCode} ({res.responseTime}ms){end}",
                            },
                            level: "warn",
                        },
                    ],
                },
                serializers: {
                    // [Request Serializer]
                    req(request) {
                        return {
                            url: request.url,
                            method: request.method,
                            id: request.id,
                            ip: request.ip,
                        };
                    },
                    // [Response Serializer]
                    res(reply) {
                        return {
                            statusCode: reply.statusCode,
                            id: reply.request?.id || "--",
                            responseTime: reply.elapsedTime?.toFixed(1) || 0,
                        };
                    },
                },
            }
        });
    }

    // [Start]
    async start() {
        this.server.log.info(`=== Starting ===\n`);
        this.registerPlugins();
        await this.registerRoutes();
        await this.server.listen({ port: this.port });
    }

    // [Stop]
    async stop() {
        await this.server.close();
    }

    // [Register Routes]
    async registerRoutes() {
        const routes = await getFileExports<WRoute>("/routes");
        if (routes.length === 0) {
            this.server.log.warn(`No routes found`);
            return;
        }
        this.server.log.info(`${routes.length} routes: ${routes.map(r => `${r.url}`).join(", ")}`);
        for (const route of routes) {
            this.server.route(route);
        }
    }

    // [Register Plugins]
    async registerPlugins() {
        // [CORS - Allow all origins]
        this.server.register(fastifyCors, { origin: "*" });

        // [Reply From - Proxy requests]
        this.server.register(fastifyReplyFrom);

        // [Helmet - Security]
        this.server.register(fastifyHelmet, { global: true });
    }

    log(message: any) {
        this.server.log.info(message);
    }

    warn(message: any) {
        this.server.log.warn(message);
    }

    error(message: any, fatal: boolean = false) {
        this.server.log.error(message);
        if (fatal) {
            process.exit(1);
        }
    }
}

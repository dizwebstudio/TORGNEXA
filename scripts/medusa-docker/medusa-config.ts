import { loadEnv, defineConfig } from '@medusajs/framework/utils'

loadEnv(process.env.NODE_ENV || 'development', process.cwd())

/**
 * Minimal config overlay used by docker-compose.medusa-test.yml.
 *
 * The official DTC starter intentionally leaves infrastructure choices to the
 * operator. This disposable stack uses plain local PostgreSQL and Redis, so
 * keep TLS disabled explicitly and pass the Redis URL to the runtime. The
 * overlay is mounted read-only and never changes the checked-out starter.
 */
module.exports = defineConfig({
  projectConfig: {
    databaseUrl: process.env.DATABASE_URL,
    databaseDriverOptions: {
      connection: {
        ssl: false,
      },
    },
    redisUrl: process.env.REDIS_URL,
    http: {
      storeCors: process.env.STORE_CORS!,
      adminCors: process.env.ADMIN_CORS!,
      authCors: process.env.AUTH_CORS!,
      jwtSecret: process.env.JWT_SECRET,
      cookieSecret: process.env.COOKIE_SECRET,
    },
  },
})

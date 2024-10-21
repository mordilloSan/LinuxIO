const express = require('express');
const next = require('next');
const session = require('express-session');
const MemoryStore = require('memorystore')(session);
const crypto = require('crypto');
const pem = require('pem');

const { router: loginRoutes, isAuthenticated } = require("./server/auth.cjs");
const { server: updateRoutes, cacheUpdateHistory } = require('./server/update.cjs');
const storageRoutes = require('./server/storage.cjs');
const { router: networkRoutes, initNetworkStats } = require('./server/network.cjs');
const { router: systemRoutes, cacheServiceDescriptions } = require('./server/systemstatus.cjs');
const systemInfoRoutes = require('./server/systeminfo.cjs');
const powerRoutes = require('./server/power.cjs');
const wireguardRoutes = require('./server/wireguard.cjs');
const { app: dockerRoutes, downloadIcons } = require("./server/docker.cjs");

const dev = process.env.NODE_ENV !== 'production';
const port = dev ? 3001 : 3000;
const app = next({ dev });
const handle = app.getRequestHandler();

app.prepare().then(async () => {
  const server = express();

  // Session middleware to manage user sessions
  server.use(session({
    secret: crypto.randomBytes(64).toString('hex'),
    resave: false,
    saveUninitialized: false,
    store: new MemoryStore({
      checkPeriod: 86400000, // prune expired entries every 24h
    }),
    cookie: {
      secure: true,
      httpOnly: true,
      maxAge: 3600000,
      sameSite: 'Strict',
      path: '/',
    },
  }));

  // Middleware to inject a timestamp into all JSON responses
  server.use((req, res, next) => {
    const originalJson = res.json;

    // Override res.json to include a timestamp
    res.json = function (body) {
      // If the body is an object, add the timestamp
      if (typeof body === 'object' && body !== null) {
        body.timestamp = new Date().toISOString();
      }
      return originalJson.call(this, body);
    };
    next();
  });

  server.use(express.json());

  server.use("/api", loginRoutes);
  server.use("/api/updates", isAuthenticated, updateRoutes);
  server.use("/api/storage", isAuthenticated, storageRoutes);
  server.use("/api/network", isAuthenticated, networkRoutes);
  server.use("/api/system-status", isAuthenticated, systemRoutes);
  server.use('/api/systeminfo', isAuthenticated, systemInfoRoutes);
  server.use('/api/power', isAuthenticated, powerRoutes);
  server.use("/api/wireguard", isAuthenticated, wireguardRoutes);
  server.use("/api/docker", isAuthenticated, dockerRoutes);

  // Call caching functions
  try {
    console.log("Starting API's");
    await cacheServiceDescriptions();
    await cacheUpdateHistory();
    await downloadIcons();
    await initNetworkStats();
  } catch (error) {
    console.error('Startup task failed:', error);
    process.exit(1); // Exit if critical startup tasks fail
  }

  // Handle all other routes with Next.js
  server.use((req, res) => handle(req, res));

  // Start server with HTTPS
  pem.createCertificate({ days: 365, selfSigned: true }, (err, keys) => {
    if (err) {
      console.error('Error generating self-signed certificate', err);
      process.exit(1);
    }
    const https = require('https');
    https.createServer({ key: keys.serviceKey, cert: keys.certificate }, server).listen(port, (err) => {
      if (err) throw err;
      console.log(`> Ready on https://localhost:${port}`);
    });
  });
});

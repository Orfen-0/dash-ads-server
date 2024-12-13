// Create root user
db.auth('root', 'example')

// Switch to your application database
db = db.getSiblingDB('dash_ads_server');

// Create collections
db.createCollection('devices');
db.createCollection('streams');

// Create indexes if needed
db.devices.createIndex({ "deviceId": 1 }, { unique: true });

// Grant roles to root user for this database
db.grantRolesToUser("root", [{ role: "readWrite", db: "dash_ads_server"}]);

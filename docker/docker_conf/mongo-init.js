db = db.getSiblingDB('dash_ads_server');

// Create collections
db.createCollection('devices');
db.createCollection('streams');
db.createCollection('events')

// Create indexes if needed
db.devices.createIndex({ "deviceId": 1 }, { unique: true });
db.devices.createIndex({ "position": "2dsphere" })

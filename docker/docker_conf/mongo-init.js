db = db.getSiblingDB('dash_ads_server');

// Create collections
db.createCollection('devices');
db.createCollection('streams');
db.createCollection('events')

// Create indexes if needed
db.devices.createIndex({ "deviceId": 1 }, { unique: true });
db.streams.createIndex({ "status": 1 });

db.devices.createIndex(
    { "lastLocation": "2dsphere" }
);
db.events.createIndex(
    { "location": "2dsphere" }
);
db.streams.createIndex(
    { "startLocation": "2dsphere" }
);

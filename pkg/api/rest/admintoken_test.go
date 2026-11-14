package rest_test

// testAdminToken is the admin credential every test handler is configured with and every admin
// request presents; it has to meet the strength rule admintoken.Parse enforces at boot.
const testAdminToken = "0123456789abcdef0123456789abcdef"

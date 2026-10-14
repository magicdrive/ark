function a() {}
function b() {}
module.exports = { a, X: 1 }
function f(k) { module.exports[k] = 1 }

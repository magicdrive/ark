function a() {}
function b() {}
module.exports = { a, X: 1 }
Object.assign(module.exports, { a: 2 })

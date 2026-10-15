function a() {}
function b() {}
module.exports = { a, X: 1 }
Reflect.defineProperty(module.exports, "a", { value: b })

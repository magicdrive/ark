function a() {}
function b() {}
module.exports = { a, X: 1 }
;/** @type {any} */ (module.exports).a = b

function a() {}
function b() {}
module.exports = { a, X: 1 }
;({ x: module.exports.a } = { x: 1 })

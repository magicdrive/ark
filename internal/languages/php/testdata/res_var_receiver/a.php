<?php
class User { public function save() {} }
class Order { public function save() {} }
function f($x) {
    $x->save();
}

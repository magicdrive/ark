<?php
namespace App\Http;

use App\B\Item;

class C
{
    public function a() { return Item::make(); }

    public function c() { return new Item(); }
}

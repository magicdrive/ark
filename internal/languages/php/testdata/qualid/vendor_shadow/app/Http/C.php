<?php
namespace App\Http;

use Illuminate\Http\Request;

class C
{
    public function a() { return Request::capture(); }

    public function b(Request $r) { return $r->input('name'); }

    public function c() { return new Request(); }

    public function d() { return \Illuminate\Http\Request::capture(); }
}

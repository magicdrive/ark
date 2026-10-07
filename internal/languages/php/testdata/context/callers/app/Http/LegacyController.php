<?php

namespace App\Http;

use App\Services\LoginScreenPolicy;

// The property is reassigned outside the constructor, so its constructor type
// is no evidence: the call stays a candidate (it could be Clinic::showsSsoButton
// as far as the source proves), so it is no edge.
class LegacyController
{
    private $policy;

    public function __construct(LoginScreenPolicy $policy)
    {
        $this->policy = $policy;
    }

    public function swap($policy)
    {
        $this->policy = $policy;
    }

    public function render()
    {
        return $this->policy->showsSsoButton();
    }
}

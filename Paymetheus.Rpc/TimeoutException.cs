// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Rpc
{
    [Serializable]
    public class TimeoutException : Exception
    {
        internal TimeoutException(string message) : base(message) { }
    }
}

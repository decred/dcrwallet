// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred
{
    public class BlockChainConsistencyException : Exception
    {
        internal BlockChainConsistencyException(string message) : base(message) { }
        internal BlockChainConsistencyException(string message, Exception innerException) : base(message, innerException) { }
    }
}

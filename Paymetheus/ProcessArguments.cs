// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using System;

namespace Paymetheus
{
    class ProcessArguments
    {
        public BlockChainIdentity IntendedNetwork { get; }

        private ProcessArguments(BlockChainIdentity intendedNetwork)
        {
            IntendedNetwork = intendedNetwork;
        }

        public static ProcessArguments ParseArguments(string[] args)
        {
            var intendedNetwork = BlockChainIdentity.MainNet;

            foreach (var arg in args)
            {
                switch (arg)
                {
                    case "-testnet":
                        intendedNetwork = BlockChainIdentity.TestNet;
                        break;
                    default:
                        throw new Exception($"Unrecognized argument `{arg}`");
                }
            }

            return new ProcessArguments(intendedNetwork);
        }
    }
}

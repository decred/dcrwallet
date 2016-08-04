// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using System;

namespace Launcher
{
    class ProcessArguments
    {
        public BlockChainIdentity IntendedNetwork { get; }
        public bool SearchPathForProcesses { get; }

        private ProcessArguments(BlockChainIdentity intendedNetwork, bool searchPathForWalletProcess)
        {
            IntendedNetwork = intendedNetwork;
            SearchPathForProcesses = searchPathForWalletProcess;
        }

        public static ProcessArguments ParseArguments(string[] args)
        {
            var intendedNetwork = BlockChainIdentity.MainNet;
            var searchPathForConsensusProcess = false;

            foreach (var arg in args)
            {
                switch (arg)
                {
                    case "-testnet":
                        intendedNetwork = BlockChainIdentity.TestNet;
                        break;
                    case "-searchpath":
                        searchPathForConsensusProcess = true;
                        break;
                    default:
                        throw new Exception($"Unrecognized argument `{arg}`");
                }
            }

            return new ProcessArguments(intendedNetwork, searchPathForConsensusProcess);
        }
    }
}

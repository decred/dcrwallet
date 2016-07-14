// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using System;
using System.Diagnostics;

namespace Paymetheus.Rpc
{
    public static class WalletProcess
    {
        public const string ProcessName = "dcrwallet";

        // Error code taken from https://msdn.microsoft.com/en-us/library/cc231199.aspx
        private const int ErrorFileNotFound = 2;

        public static Process Start(BlockChainIdentity intendedNetwork, string appDataDirectory, string executablePath = null)
        {
            if (intendedNetwork == null)
                throw new ArgumentNullException(nameof(intendedNetwork));
            if (appDataDirectory == null)
                throw new ArgumentNullException(nameof(appDataDirectory));

            var networkFlag = "";
            if (intendedNetwork != BlockChainIdentity.MainNet)
            {
                networkFlag = $"--{intendedNetwork.Name}";
            }

            var v4ListenAddress = RpcListenAddress("127.0.0.1", intendedNetwork);

            var processInfo = new ProcessStartInfo();
            processInfo.FileName = executablePath ?? ProcessName;
            processInfo.Arguments = $"{networkFlag} --noinitialload --experimentalrpclisten={v4ListenAddress} --onetimetlskey --appdata=\"{appDataDirectory}\"";
            processInfo.UseShellExecute = false;
            processInfo.RedirectStandardError = true;
            processInfo.RedirectStandardOutput = true;
            processInfo.CreateNoWindow = true;

            try
            {
                var process = Process.Start(processInfo);
                process.ErrorDataReceived += (sender, args) => Console.WriteLine("err> {0}", args.Data);
                process.OutputDataReceived += (sender, args) => Console.WriteLine("{0}", args.Data);
                process.BeginErrorReadLine();
                process.BeginOutputReadLine();
                return process;
            }
            catch (System.ComponentModel.Win32Exception w32ex) when (w32ex.NativeErrorCode == ErrorFileNotFound)
            {
                throw new ProcessNotFoundException(processInfo.FileName, w32ex);
            }
        }

        public static string RpcListenAddress(string hostnameOrIp, BlockChainIdentity intendedNetwork)
        {
            if (intendedNetwork == null)
                throw new ArgumentNullException(nameof(intendedNetwork));

            string port;
            if (intendedNetwork == BlockChainIdentity.MainNet)
                port = "9110";
            else if (intendedNetwork == BlockChainIdentity.TestNet)
                port = "19110";
            else if (intendedNetwork == BlockChainIdentity.SimNet)
                port = "19557";
            else
                throw new UnknownBlockChainException(intendedNetwork);

            return $"{hostnameOrIp}:{port}";
        }
    }
}

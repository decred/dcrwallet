// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Rpc
{
    public class ProcessNotFoundException : Exception
    {
        public ProcessNotFoundException(string processNameOrPath, Exception innerException = null)
            : base($"The process `{processNameOrPath}` could not be started because it was not found", innerException) { }
    }
}

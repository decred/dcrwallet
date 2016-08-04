// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus.Framework
{
    public class WizardDialogViewModelBase : ViewModelBase
    {
        public WizardDialogViewModelBase(WizardViewModelBase wizard) : base()
        {
            _wizard = wizard;
        }

        protected WizardViewModelBase _wizard;
    }
}
